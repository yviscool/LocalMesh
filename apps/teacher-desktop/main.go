// LocalMesh 课堂 — native teacher console built with mygo.
//
// The console owns every classroom manager in-process, serves a minimal HTTP
// surface for student agents (thumbnail push), listens for UDP discovery
// announcements, and exposes typed bindings plus realtime events to the
// embedded frontend.
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"localmesh/internal/storage/sqlite"

	"localmesh/internal/capabilities/attendance"
	"localmesh/internal/capabilities/audio"
	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/inventory"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/quiz"
	"localmesh/internal/capabilities/remoteshell"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screen"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/whiteboard"
	"localmesh/internal/classroom"
	"localmesh/internal/domain/discovery"
	"localmesh/internal/platform/common"
	udpdiscovery "localmesh/internal/transport/discovery"

	"localmesh/internal/desktopconfig"
	"localmesh/internal/logging"

	"github.com/egoist/mygo"
)

const (
	defaultHTTPListen      = "127.0.0.1:8080"
	defaultDiscoveryListen = ":7444"
	defaultClassroomID     = "CLASS-DEFAULT"
)

// Console owns every manager the bindings and events operate on. Mutable
// session state (quiz, attendance, channel, audio) is guarded by mu. The
// quitting flag is only touched on the main thread.
type Console struct {
	mu            sync.Mutex
	quitting      bool
	quizSession   *quiz.QuizSession
	attendance    *attendance.AttendanceSession
	channelID     int
	audioCancel   context.CancelFunc
	audioCapturer audio.Capturer
	audioSender   *audio.Sender

	discoveryReg *discovery.Registry
	chatMgr      *interaction.ChatManager
	raiseHandQ   *interaction.RaiseHandQueue
	seating      *interaction.SeatingChart
	thumbs       *screen.ThumbnailCache
	powerSvc     *power.Service
	lockMgr      *screenlock.Manager
	procMon      *restrictions.ProcessMonitor
	nfMgr        *netfilter.Manager
	invMgr       *inventory.Manager
	buzzer       *quiz.BuzzerSession
	broadcast    *broadcast.BroadcastSession
	whiteboard   *whiteboard.Whiteboard
	shell        *remoteshell.Runner
	store        *sqlite.Store
	hub          *agentHub
	cfg          desktopconfig.Config
	groupCast    *GroupCastManager
	screenStream *screenStream
	inputMu      sync.Mutex
	inputConns   map[string]*inputConn
	classActive  bool

	win  *mygo.Window
	tray *mygo.Tray
}

// NewConsole wires the full manager stack, mirroring the construction the
// HTTP console previously performed by the legacy web console.
func NewConsole(cfg desktopconfig.Config) (*Console, error) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		return nil, fmt.Errorf("platform runtime: %w", err)
	}

	discReg := discovery.NewRegistry()
	chatMgr := interaction.NewChatManager()
	raiseHandQ := interaction.NewRaiseHandQueue()
	seating, err := interaction.NewSeatingChart(defaultClassroomID, "MainLab", 8, 8)
	if err != nil {
		return nil, fmt.Errorf("seating chart: %w", err)
	}
	thumbs := screen.NewThumbnailCache(320, 180, 10*time.Second)
	lockMgr := screenlock.NewManager(screenlock.NewPlatformTrapper())
	powerSvc := power.NewService(&power.OSCommandExecutor{OS: string(rt.OS)}, nil)
	procMon := restrictions.NewProcessMonitor(&restrictions.OSProcessController{}, nil)

	var nfAdapter netfilter.FirewallAdapter
	if rt.OS == "windows" {
		nfAdapter = netfilter.NewWindowsNetshAdapter()
	} else {
		nfAdapter = netfilter.NewMockFirewallAdapter()
	}

	invMgr, err := inventory.NewManager(inventory.NewDefaultCollector())
	if err != nil {
		return nil, fmt.Errorf("inventory manager: %w", err)
	}

	// SQLite stores agent-reported audit events and issued commands.
	if err := os.MkdirAll("data", 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	storeCtx, storeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer storeCancel()
	store, err := sqlite.Open(storeCtx, "data/teacher-desktop.db")
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}
	if err := store.Migrate(storeCtx); err != nil {
		return nil, fmt.Errorf("migrate sqlite store: %w", err)
	}

	console := &Console{
		channelID:    classroom.DefaultChannelID,
		discoveryReg: discReg,
		chatMgr:      chatMgr,
		raiseHandQ:   raiseHandQ,
		seating:      seating,
		thumbs:       thumbs,
		powerSvc:     powerSvc,
		lockMgr:      lockMgr,
		procMon:      procMon,
		nfMgr:        netfilter.NewManager(nfAdapter),
		invMgr:       invMgr,
		buzzer:       quiz.NewBuzzerSession(),
		broadcast:    broadcast.NewBroadcastSession(defaultClassroomID),
		whiteboard:   whiteboard.NewWhiteboard(defaultClassroomID),
		shell:        remoteshell.NewRunner(),
		store:        store,
		hub:          newAgentHub(),
	}
	// 学生机跟随教师频道；离线守护默认开启（宽限 10 秒）。
	console.hub.setChannel(classroom.DefaultChannelID)
	console.hub.setOffline(&OfflinePolicy{Enabled: true, LeaseSeconds: cfg.Offline.LeaseSeconds})
	console.cfg = cfg
	console.hub.setAgentConfig(&AgentConfigSection{
		ThumbnailIntervalMS: cfg.Thumbnails.IntervalMS,
		ThumbnailQuality:    cfg.Thumbnails.Quality,
	})

	console.groupCast = &GroupCastManager{c: console}

	// 回载持久化座位表
	seats, err := store.ListSeats(storeCtx, defaultClassroomID)
	if err == nil {
		for _, seat := range seats {
			if seat.DeviceID != "" {
				_ = seating.AssignSeat(seat.Row, seat.Col, seat.DeviceID, seat.StudentName)
			}
		}
	}
	return console, nil
}

func main() {
	cfg, err := desktopconfig.Load(desktopconfig.Path())
	if err != nil {
		log.Printf("[Config] load failed, using defaults: %v", err)
	}
	var (
		flagHTTP      = flag.String("http", fmt.Sprintf("127.0.0.1:%d", cfg.Network.HTTPPort), "HTTP listener for student agent pushes")
		flagDiscovery = flag.String("discovery", fmt.Sprintf(":%d", cfg.Network.DiscoveryPort), "UDP discovery listener address")
	)
	flag.Parse()

	// Single instance: a second launch hands over to the running one.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	if closer := logging.Setup(cfg.Log.Level, cfg.Log.File); closer != nil {
		defer closer()
	}

	console, err := NewConsole(cfg)
	if err != nil {
		log.Fatalf("failed to build console: %v", err)
	}

	go console.serveHTTP(*flagHTTP)
	go console.serveDiscovery(*flagDiscovery)

	// Typed bindings consumed by the frontend via src/mygo.ts.
	mygo.Bind(&ClassroomService{c: console})
	mygo.Bind(&ControlService{c: console})
	mygo.Bind(&InteractionService{c: console})
	mygo.Bind(&TeachingService{c: console})
	mygo.Bind(&BroadcastService{c: console})
	mygo.Bind(&ToolsService{c: console})
	mygo.Bind(&MonitorService{c: console})
	mygo.Bind(&WindowService{c: console})
	mygo.Bind(&AuditService{c: console})
	mygo.Bind(&FilesService{c: console})
	mygo.Bind(&RosterService{c: console})
	mygo.Bind(&GroupService{c: console})
	mygo.Bind(&SceneService{c: console})
	mygo.Bind(&ConfigService{c: console})
	mygo.Bind(&GroupCastService{c: console})

	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		console.showMainWindow()
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "LocalMesh 课堂",
			Width:           1440,
			Height:          900,
			MinWidth:        1080,
			MinHeight:       700,
			Maximized:       true,
			TitleBarStyle:   mygo.TitleBarHidden,
			TitleBarHeight:  42,
			BackgroundColor: "#0b0e14",
			StateKey:        "teacher-main",
			URL:             "/",
		})
		console.win = win
		console.setupTray()
		console.startWatchers()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// serveHTTP exposes the agent-facing surface: thumbnail push, student-side
// interaction endpoints (chat, raise hand, buzzer, quiz submission, check-in)
// and health/candidates for debugging.
func (c *Console) serveHTTP(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/thumbnails/push", c.handlePushThumbnail)
	mux.HandleFunc("GET /api/v1/candidates", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, c.deviceList())
	})
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "healthy", "component": "localmesh-teacher-desktop"})
	})

	// Student-side endpoints so agents (or curl simulations) can interact.
	mux.HandleFunc("POST /api/v1/agent/poll", c.handleAgentPoll)
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, _ *http.Request) {
		groups, members, err := c.store.ListGroups(context.Background())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type groupView struct {
			GroupID string   `json:"group_id"`
			Members []string `json:"members"`
		}
		out := make([]groupView, 0, len(groups))
		for _, g := range groups {
			out = append(out, groupView{GroupID: g.GroupID, Members: members[g.GroupID]})
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /files/", c.handleServeFile)
	mux.HandleFunc("POST /api/v1/files/collect", c.handleCollectUpload)
	mux.HandleFunc("POST /api/v1/chat/messages", c.handleStudentChat)
	mux.HandleFunc("POST /api/v1/raisehand", c.handleRaiseHand)
	mux.HandleFunc("POST /api/v1/buzzer/press", c.handleBuzzerPress)
	mux.HandleFunc("POST /api/v1/quiz/submit", c.handleQuizSubmit)
	mux.HandleFunc("POST /api/v1/attendance/checkin", c.handleAttendanceCheckIn)

	// Debug views of the interaction state.
	mux.HandleFunc("GET /api/v1/chat/messages", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, c.chatMgr.GetHistory(chatFilter()))
	})
	mux.HandleFunc("GET /api/v1/raisehand", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, c.raiseHandQ.ListActive(defaultClassroomID))
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	log.Printf("[HTTP] agent endpoint listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[HTTP] server error: %v", err)
	}
}

// serveDiscovery binds the UDP announcement listener feeding the registry.
func (c *Console) serveDiscovery(addr string) {
	uaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Printf("[Discovery] invalid address %s: %v", addr, err)
		return
	}
	srv, err := udpdiscovery.Listen(uaddr, c.discoveryReg)
	if err != nil {
		log.Printf("[Discovery] listener failed to bind %s: %v", addr, err)
		return
	}
	log.Printf("[Discovery] UDP listener active on %s", addr)
	_ = srv.Serve(context.Background())
}

// deviceList snapshots the current discovery registry as view models,
// filtered to the active classroom channel (legacy agents always shown).
func (c *Console) deviceList() []DeviceInfo {
	if c.discoveryReg == nil {
		return []DeviceInfo{}
	}
	c.mu.Lock()
	channel := c.channelID
	c.mu.Unlock()

	candidates := c.discoveryReg.Candidates(time.Now(), 60*time.Second)
	out := make([]DeviceInfo, 0, len(candidates))
	for _, cand := range candidates {
		if cand.ChannelID != 0 && cand.ChannelID != channel {
			continue
		}
		out = append(out, DeviceInfo{
			DeviceID:  string(cand.DeviceID),
			Endpoint:  cand.Address.String(),
			Method:    string(cand.Method),
			SeenAt:    cand.SeenAt.Format(time.RFC3339),
			ChannelID: cand.ChannelID,
		})
	}
	return out
}

// handlePushThumbnail stores a JPEG frame pushed by a student agent.
func (c *Console) handlePushThumbnail(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Device-ID")
	if deviceID == "" {
		http.Error(w, "missing X-Device-ID header", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxThumbnailPushBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("read frame body: %v", err), http.StatusBadRequest)
		return
	}

	seq, _ := strconv.ParseUint(r.Header.Get("X-Frame-Seq"), 10, 64)
	if _, err := c.thumbs.UpdateRaw(deviceID, body, screen.FormatJPEG, 0, 0, seq); err != nil {
		http.Error(w, fmt.Sprintf("store thumbnail: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

const maxThumbnailPushBytes = 2 << 20 // 2 MB

func chatFilter() interaction.ChatFilter {
	return interaction.ChatFilter{ClassroomID: defaultClassroomID, UserID: "teacher"}
}

func writeJSON(w http.ResponseWriter, code int, val any, gzipOK ...bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(gzipOK) > 0 && gzipOK[0] {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(code)
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_ = json.NewEncoder(gz).Encode(val)
		return
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(val)
}
