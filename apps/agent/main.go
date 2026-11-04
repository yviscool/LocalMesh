package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"localmesh/internal/capabilities/audio"
	"localmesh/internal/capabilities/filetransfer"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/registry"
	"localmesh/internal/capabilities/remoteshell"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/watchdog"
	"localmesh/internal/capabilities/workers"
	"localmesh/internal/classroom"
	"localmesh/internal/domain/identity"
	"localmesh/internal/platform"
	"localmesh/internal/platform/common"
	winplat "localmesh/internal/platform/windows"
	udpdiscovery "localmesh/internal/transport/discovery"
	"localmesh/internal/transport/ipc"
)

const (
	defaultAgentMutex    = "LocalMeshStudentAgentMutex"
	defaultDiscoveryAddr = "255.255.255.255:7444"
	defaultWindowsPipe   = `\\.\pipe\localmesh-agent`
	defaultUnixSocket    = "/tmp/localmesh-agent.sock"
	defaultTeacherURL    = "http://127.0.0.1:8080"
)

type AgentApp struct {
	mu               sync.RWMutex
	deviceID         identity.DeviceID
	pubKey           ed25519.PublicKey
	privKey          ed25519.PrivateKey
	platformRT       platform.Runtime
	capRegistry      *registry.Registry
	supervisor       *workers.Supervisor
	lockMgr          *screenlock.Manager
	powerSvc         *power.Service
	processMon       *restrictions.ProcessMonitor
	fileRcvr         *filetransfer.Receiver
	offlineGuard     *watchdog.OfflineGuard
	exitPasswordHash string
	exitChan         chan struct{}
	exitOnce         sync.Once
	ipcListener      ipc.Listener
	pipeAddress      string
	discoveryAddr    string
	teacherURL       string
	screenHTTP       string
	screenSrv        *screenServer
	channelID        int
	nfMgr            *netfilter.Manager
	scriptRunner     *remoteshell.Runner
	systemPolicyState restrictions.SystemPolicy
	systemExecutor    restrictions.SystemPolicyExecutor
	remoteGate       remoteControlGate
	broadcastRX      *broadcastReceiver
	feed             *broadcastFeed
	publisher        broadcastPublisher
	events           eventBuffer
	thumbCtl         thumbnailController
	thumbMu          sync.Mutex
	lastThumb        []byte
	ackedMu          sync.Mutex
	acked            []string
	audioCancel      context.CancelFunc
	audioCapturer    audio.Capturer
	audioSender      *audio.Sender
	discoveryStop    chan struct{}
	releaseMutex     func()
}

// ack records a command acknowledgement for the next poll.
func (a *AgentApp) ack(id string) {
	a.ackedMu.Lock()
	a.acked = append(a.acked, id)
	a.ackedMu.Unlock()
}

// drainAcked returns and clears pending acknowledgements.
func (a *AgentApp) drainAcked() []string {
	a.ackedMu.Lock()
	defer a.ackedMu.Unlock()
	out := a.acked
	a.acked = nil
	return out
}

func main() {
	var (
		flagID                 = flag.String("id", "", "Stable device identifier (min 8 chars)")
		flagPipe               = flag.String("pipe", "", "IPC endpoint address (named pipe or unix socket)")
		flagDiscovery          = flag.Bool("discovery", true, "Enable UDP broadcast announcement")
		flagDiscoveryAddr      = flag.String("discovery-addr", defaultDiscoveryAddr, "UDP discovery broadcast address")
		flagTeacherURL         = flag.String("teacher-url", defaultTeacherURL, "Teacher console HTTP base URL for thumbnail push")
		flagScreenHTTP         = flag.String("screen-http", DefaultScreenHTTPAddr, "On-demand HD screen endpoint (empty disables)")
		flagInstallAutostart   = flag.Bool("install-autostart", false, "Configure agent to launch automatically on logon")
		flagUninstallAutostart = flag.Bool("uninstall-autostart", false, "Remove autostart logon configuration")
		flagNoMutex            = flag.Bool("no-single-instance", false, "Skip single-instance mutex check")

		// Windows Service flags
		flagServiceInstall   = flag.Bool("service-install", false, "Install agent as Windows LocalSystem service")
		flagServiceUninstall = flag.Bool("service-uninstall", false, "Uninstall agent from Windows services")
		flagServiceRun       = flag.Bool("service-run", false, "Run agent in Windows service mode under SCM")

		// Exit password protection flags
		flagExit         = flag.Bool("exit", false, "Request running agent to shut down (requires -password)")
		flagPassword     = flag.String("password", "", "Password for -exit command")
		flagExitPassword = flag.String("exit-password", DefaultExitPassword, "Set administrator exit password")
	)
	flag.Parse()

	// Platform runtime bootstrap
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		log.Fatalf("failed to initialize platform runtime: %v", err)
	}

	pipeAddr := *flagPipe
	if pipeAddr == "" {
		if rt.OS == platform.Windows {
			pipeAddr = defaultWindowsPipe
		} else {
			pipeAddr = defaultUnixSocket
		}
	}

	// 1. Handle remote exit request via IPC
	if *flagExit {
		if err := requestAgentExit(rt, pipeAddr, *flagPassword); err != nil {
			log.Fatalf("Agent shutdown rejected: %v", err)
		}
		fmt.Println("Agent shutdown command accepted and processed successfully.")
		return
	}

	// 2. Handle Windows Service Installation & Removal
	if *flagServiceInstall {
		execPath, err := os.Executable()
		if err != nil {
			log.Fatalf("failed to locate executable: %v", err)
		}
		if err := InstallService(execPath, []string{"-service-run"}); err != nil {
			log.Fatalf("Service installation failed: %v", err)
		}
		fmt.Printf("Service %s installed successfully as LocalSystem.\n", ServiceName)
		return
	}

	if *flagServiceUninstall {
		if err := UninstallService(); err != nil {
			log.Fatalf("Service uninstallation failed: %v", err)
		}
		fmt.Printf("Service %s uninstalled successfully.\n", ServiceName)
		return
	}

	// 3. Autostart management flags
	if *flagInstallAutostart {
		execPath, err := os.Executable()
		if err != nil {
			log.Fatalf("failed to locate executable: %v", err)
		}
		if runtime.GOOS == "windows" {
			if err := winplat.SetAutostart("LocalMeshAgent", execPath, true); err != nil {
				log.Fatalf("failed to enable autostart: %v", err)
			}
			fmt.Println("Autostart successfully registered in Windows Registry.")
		} else {
			fmt.Println("Autostart registration is currently native on Windows.")
		}
		return
	}

	if *flagUninstallAutostart {
		if runtime.GOOS == "windows" {
			if err := winplat.SetAutostart("LocalMeshAgent", "", false); err != nil {
				log.Fatalf("failed to disable autostart: %v", err)
			}
			fmt.Println("Autostart successfully removed.")
		}
		return
	}

	// 4. Windows Service Execution Mode
	if *flagServiceRun || IsWindowsService() {
		log.Printf("Starting %s as Windows Service...", ServiceName)
		var app *AgentApp
		err := RunService(func(svcCtx context.Context) error {
			var initErr error
			app, initErr = NewAgentApp(rt, *flagID, pipeAddr, *flagDiscoveryAddr, nil)
			if initErr != nil {
				return initErr
			}
			app.SetExitPassword(*flagExitPassword)
			app.SetTeacherURL(*flagTeacherURL)
			app.SetScreenHTTP(*flagScreenHTTP)
			if err := app.Start(svcCtx, *flagDiscovery); err != nil {
				return err
			}
			go func() {
				select {
				case <-svcCtx.Done():
				case <-app.ExitChan():
				}
				app.Stop()
			}()
			return nil
		}, func() {
			if app != nil {
				app.Stop()
			}
		})
		if err != nil {
			log.Fatalf("Windows service error: %v", err)
		}
		return
	}

	// 5. Interactive Foreground Execution Mode
	var releaseMutex func()
	if !*flagNoMutex && rt.OS == platform.Windows {
		var err error
		releaseMutex, err = winplat.EnsureSingleInstance(defaultAgentMutex)
		if err != nil {
			log.Fatalf("agent startup rejected: %v (another instance is already running)", err)
		}
		defer releaseMutex()

		// Anti-tamper armor: Harden process DACL against termination
		_ = watchdog.HardenProcessDACL(uint32(os.Getpid()))
	}

	app, err := NewAgentApp(rt, *flagID, pipeAddr, *flagDiscoveryAddr, releaseMutex)
	if err != nil {
		log.Fatalf("failed to initialize agent: %v", err)
	}
	app.SetExitPassword(*flagExitPassword)
	app.SetTeacherURL(*flagTeacherURL)
	app.SetScreenHTTP(*flagScreenHTTP)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.Start(ctx, *flagDiscovery); err != nil {
		log.Fatalf("agent error: %v", err)
	}

	select {
	case <-ctx.Done():
		log.Println("Received termination signal.")
	case <-app.ExitChan():
		log.Println("Authorized administrator exit triggered.")
	}

	log.Println("Shutting down LocalMesh Student Agent gracefully...")
	app.Stop()
	log.Println("LocalMesh Student Agent stopped.")
}

func requestAgentExit(rt platform.Runtime, pipeAddr, password string) error {
	dialCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := rt.Endpoint.Dial(dialCtx, pipeAddr)
	if err != nil {
		return fmt.Errorf("dial agent endpoint (%s): %w", pipeAddr, err)
	}
	defer conn.Close()

	req := ipc.Request{
		ID:        fmt.Sprintf("exit-%d", time.Now().UnixNano()),
		MessageID: "msg-exit",
		Action:    "agent.exit",
		Payload: map[string]any{
			"password": password,
		},
	}
	if err := ipc.Write(conn, req, ipc.DefaultMaxMessage); err != nil {
		return fmt.Errorf("send exit request: %w", err)
	}

	var resp ipc.Response
	if err := ipc.Read(conn, &resp, ipc.DefaultMaxMessage); err != nil {
		return fmt.Errorf("read exit response: %w", err)
	}

	if !resp.OK {
		errMsg := resp.Code
		if pErr, ok := resp.Payload["error"].(string); ok && pErr != "" {
			errMsg = pErr
		}
		return errors.New(errMsg)
	}
	return nil
}

func NewAgentApp(rt platform.Runtime, customID string, pipeAddr, discoveryAddr string, releaseMutex func()) (*AgentApp, error) {
	var devID identity.DeviceID
	if customID != "" {
		var err error
		devID, err = identity.NewDeviceID(customID)
		if err != nil {
			return nil, fmt.Errorf("invalid device id: %w", err)
		}
	} else {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "StudentPC"
		}
		cleanHost := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			return '-'
		}, hostname)
		if len(cleanHost) < 8 {
			cleanHost = fmt.Sprintf("DEVICE-%s-01", cleanHost)
		}
		var err error
		devID, err = identity.NewDeviceID(cleanHost)
		if err != nil {
			devID = identity.DeviceID("DEVICE-LOCAL-STUDENT-01")
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate device key: %w", err)
	}

	capReg, err := registry.Default(rt.OS)
	if err != nil {
		return nil, fmt.Errorf("init capability registry: %w", err)
	}

	sup := workers.New(func(capID string) (workers.Worker, error) {
		return &noopWorker{id: capID}, nil
	})

	lockMgr := screenlock.NewManagerWithOverlay(screenlock.NewPlatformTrapper(), screenlock.NewPlatformOverlay())
	powerSvc := power.NewService(&power.OSCommandExecutor{OS: string(rt.OS)}, nil)
	procMon := restrictions.NewProcessMonitor(&restrictions.OSProcessController{}, nil)
	fileRcvr := filetransfer.NewReceiver()

	var nfAdapter netfilter.FirewallAdapter
	if rt.OS == "windows" {
		nfAdapter = netfilter.NewWindowsNetshAdapter()
	} else {
		nfAdapter = netfilter.NewMockFirewallAdapter()
	}

	offlineGuard := watchdog.NewOfflineGuard(lockMgr, watchdog.DefaultOfflineGuardConfig())

	return &AgentApp{
		deviceID:         devID,
		pubKey:           pub,
		privKey:          priv,
		platformRT:       rt,
		capRegistry:      capReg,
		supervisor:       sup,
		lockMgr:          lockMgr,
		powerSvc:         powerSvc,
		processMon:       procMon,
		fileRcvr:         fileRcvr,
		offlineGuard:     offlineGuard,
		nfMgr:            netfilter.NewManager(nfAdapter),
		scriptRunner:     remoteshell.NewRunner(),
		channelID:        classroom.DefaultChannelID,
		exitPasswordHash: HashPassword(DefaultExitPassword),
		exitChan:         make(chan struct{}),
		pipeAddress:      pipeAddr,
		discoveryAddr:    discoveryAddr,
		discoveryStop:    make(chan struct{}),
		releaseMutex:     releaseMutex,
	}, nil
}

// SetExitPassword updates the administrator exit password or hash.
func (a *AgentApp) SetExitPassword(pwd string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exitPasswordHash = HashPassword(pwd)
}

// VerifyExitPassword validates the candidate password against the current password hash.
func (a *AgentApp) VerifyExitPassword(candidate string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return VerifyExitPassword(candidate, a.exitPasswordHash)
}

// SetTeacherURL configures the teacher console endpoint for thumbnail push.
// Must be called before Start.
func (a *AgentApp) SetTeacherURL(url string) {
	a.teacherURL = url
}

// SetScreenHTTP configures the on-demand HD screen endpoint address.
// Must be called before Start.
func (a *AgentApp) SetScreenHTTP(addr string) {
	a.screenHTTP = addr
}

// ExitChan returns a channel that is closed when an authorized exit is requested.
func (a *AgentApp) ExitChan() <-chan struct{} {
	return a.exitChan
}

// TriggerExit initiates an authorized shutdown.
func (a *AgentApp) TriggerExit() {
	a.exitOnce.Do(func() {
		close(a.exitChan)
	})
}

func (a *AgentApp) Start(ctx context.Context, enableDiscovery bool) error {
	log.Printf("Starting LocalMesh Student Agent [ID: %s, OS: %s]", a.deviceID, a.platformRT.OS)

	// 1. Start local IPC listener (Named Pipe or Unix domain socket)
	listener, err := a.platformRT.Endpoint.Listen(ctx, a.pipeAddress)
	if err != nil {
		return fmt.Errorf("listen local endpoint (%s): %w", a.pipeAddress, err)
	}
	a.ipcListener = listener
	log.Printf("Local IPC endpoint active on %s", a.pipeAddress)

	go a.serveIPC(ctx)

	// 2. Offline watchdog: armed lazily by the poller when class control
	// arrives; here we just record it for later replacement.
	// (a.offlineGuard starts inside startPoller on first state.)

	// 3. Start UDP Discovery Broadcaster
	if enableDiscovery {
		go a.startDiscoveryAnnouncer(ctx)
	}

	// 4. Start background classroom audio stream listener & low-latency player
	a.restartAudioReceiver()

	// 5. Start on-demand HD screen endpoint (teacher pulls full frames) and
	// the classroom broadcast receiver (feeds /broadcast.mjpg for the UI)
	a.feed = newBroadcastFeed()
	a.feed.setNotifier(a.notifyStudentUI)
	a.broadcastRX = newBroadcastReceiver(a.feed)
	a.broadcastRX.retune(classroom.DefaultChannelID)
	if a.screenHTTP != "" {
		a.screenSrv = newScreenServer(a.screenHTTP, a.feed)
		a.screenSrv.Start()
	}

	// 6. Start screen thumbnail pusher (feeds the teacher console live preview)
	if a.teacherURL != "" {
		go a.startThumbnailPusher(ctx, a.teacherURL)
	}

	// 7. Remote control input channel (gated by teacher command)
	if err := a.startRemoteControlServer(ctx); err != nil {
		log.Printf("[RemoteCtl] %v", err)
	}

	// 8. Command channel: poll the teacher for desired state + commands
	if a.teacherURL != "" {
		go a.startPoller(ctx)
	}

	return nil
}

// restartAudioReceiver (re)binds the classroom audio listener on the
// multicast address derived from the current channel.
func (a *AgentApp) restartAudioReceiver() {
	a.mu.Lock()
	if a.audioCancel != nil {
		a.audioCancel()
	}
	audioCtx, cancel := context.WithCancel(context.Background())
	a.audioCancel = cancel
	channel := a.channelID
	a.mu.Unlock()

	go func() {
		addr := classroom.DeriveMulticastAddr(channel, audio.DefaultAudioMulticastAddr)
		receiver, err := audio.NewReceiver(audio.ReceiverConfig{ListenAddr: addr})
		if err != nil {
			log.Printf("[Audio] Background receiver setup skipped: %v", err)
			return
		}
		defer receiver.Close()

		player := audio.NewPlayer(audio.DefaultAudioFormat)
		if err := player.Start(audioCtx); err != nil {
			log.Printf("[Audio] Background player start failed: %v", err)
			return
		}
		defer player.Stop()

		log.Printf("[Audio] Listening for classroom audio on %s", addr)
		if err := receiver.Stream(audioCtx, player); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("[Audio] Stream finished: %v", err)
		}
	}()
}

func (a *AgentApp) Stop() {
	if a.screenSrv != nil {
		a.screenSrv.Stop()
	}
	if a.broadcastRX != nil {
		a.broadcastRX.stop()
	}

	if a.discoveryStop != nil {
		select {
		case <-a.discoveryStop:
		default:
			close(a.discoveryStop)
		}
	}

	if a.offlineGuard != nil {
		a.offlineGuard.Stop()
	}

	if a.ipcListener != nil {
		_ = a.ipcListener.Close()
	}

	if a.lockMgr != nil {
		_ = a.lockMgr.Unlock()
	}

	if a.supervisor != nil {
		a.supervisor.StopAll()
	}

	if a.releaseMutex != nil {
		a.releaseMutex()
	}
}

func (a *AgentApp) serveIPC(ctx context.Context) {
	for {
		conn, err := a.ipcListener.Accept(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("IPC accept error: %v", err)
				return
			}
		}

		go func(c io.ReadWriteCloser) {
			defer c.Close()
			for {
				var req ipc.Request
				if err := ipc.Read(c, &req, ipc.DefaultMaxMessage); err != nil {
					return
				}

				switch req.Action {
				case "agent.exit":
					pwd, _ := req.Payload["password"].(string)
					if !a.VerifyExitPassword(pwd) {
						resp := ipc.Response{
							ID:   req.ID,
							OK:   false,
							Code: "forbidden",
							Payload: map[string]any{
								"error": "invalid exit password, permission denied",
							},
						}
						_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)
						continue
					}
					resp := ipc.Response{
						ID:   req.ID,
						OK:   true,
						Code: "ok",
						Payload: map[string]any{
							"status": "shutting_down",
						},
					}
					_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)
					go func() {
						time.Sleep(100 * time.Millisecond)
						a.TriggerExit()
					}()
					return

				case "agent.heartbeat":
					if a.offlineGuard != nil {
						a.offlineGuard.RecordHeartbeat()
					}
					resp := ipc.Response{
						ID:   req.ID,
						OK:   true,
						Code: "ok",
						Payload: map[string]any{
							"heartbeat_recorded": true,
						},
					}
					_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)

				case "agent.session.start":
					sessID, _ := req.Payload["session_id"].(string)
					if a.offlineGuard != nil {
						a.offlineGuard.EnterSession(sessID)
					}
					resp := ipc.Response{
						ID:   req.ID,
						OK:   true,
						Code: "ok",
						Payload: map[string]any{
							"session_started": sessID,
						},
					}
					_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)

				case "agent.session.end":
					if a.offlineGuard != nil {
						a.offlineGuard.LeaveSession()
					}
					resp := ipc.Response{
						ID:   req.ID,
						OK:   true,
						Code: "ok",
						Payload: map[string]any{
							"session_ended": true,
						},
					}
					_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)

				default:
					resp := ipc.Response{
						ID:   req.ID,
						OK:   true,
						Code: "ok",
						Payload: map[string]any{
							"device_id": string(a.deviceID),
							"timestamp": time.Now().UTC().Format(time.RFC3339),
						},
					}
					_ = ipc.Write(c, resp, ipc.DefaultMaxMessage)
				}
			}
		}(conn)
	}
}

func (a *AgentApp) startDiscoveryAnnouncer(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	raddr, err := net.ResolveUDPAddr("udp", a.discoveryAddr)
	if err != nil {
		log.Printf("Discovery disabled: failed to resolve %s: %v", a.discoveryAddr, err)
		return
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		log.Printf("Discovery broadcast dial error: %v", err)
		return
	}
	defer conn.Close()

	caps := []string{
		"classroom.control",
		"screenlock.lock",
		"power.shutdown",
		"power.reboot",
		"restrictions.process",
		"file.distribute",
		"quiz.session",
		"quiz.buzzer",
		"netfilter.firewall",
		"broadcast.stream",
		"watchdog.protect",
		"whiteboard.draw",
		"remoteshell.exec",
		"inventory.collect",
		"attendance.rollcall",
	}

	// Announcement built per tick so a channel follow takes effect without
	// restarting the announcer.
	log.Printf("UDP Discovery announcer broadcasting to %s", a.discoveryAddr)

	_, _ = conn.Write(a.buildAnnouncement(caps))

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.discoveryStop:
			return
		case <-ticker.C:
			_, _ = conn.Write(a.buildAnnouncement(caps))
		}
	}
}

// buildAnnouncement marshals the current discovery announcement.
func (a *AgentApp) buildAnnouncement(caps []string) []byte {
	a.mu.RLock()
	channel := a.channelID
	a.mu.RUnlock()

	ann := udpdiscovery.Announcement{
		Version:      udpdiscovery.ProtocolVersion,
		DeviceID:     string(a.deviceID),
		Endpoint:     a.pipeAddress,
		Capabilities: caps,
		ChannelID:    channel,
		TTLSeconds:   30,
	}
	payload, err := json.Marshal(ann)
	if err != nil {
		return nil
	}
	return payload
}

type noopWorker struct {
	id string
}

func (w *noopWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (w *noopWorker) Stop() error {
	return nil
}
