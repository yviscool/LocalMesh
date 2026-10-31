package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"localmesh/internal/capabilities/interaction"
	"localmesh/internal/capabilities/inventory"
	"localmesh/internal/capabilities/netfilter"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screen"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/domain/discovery"
	"localmesh/internal/platform"
	"localmesh/internal/platform/common"
	winplat "localmesh/internal/platform/windows"
	"localmesh/internal/storage/sqlite"
	udpdiscovery "localmesh/internal/transport/discovery"
)

const (
	defaultTeacherMutex    = "LocalMeshTeacherConsoleMutex"
	defaultHTTPListen      = "127.0.0.1:8080"
	defaultDiscoveryListen = ":7444"
)

type TeacherApp struct {
	platformRT   platform.Runtime
	store        *sqlite.Store
	discoveryReg *discovery.Registry
	udpServer    *udpdiscovery.Server
	apiServer    *APIServer
	httpServer   *http.Server
	releaseMutex func()
}

func main() {
	var (
		flagHTTP      = flag.String("http", defaultHTTPListen, "HTTP web console listen address")
		flagDiscovery = flag.String("discovery", defaultDiscoveryListen, "UDP discovery listener address")
		flagDB        = flag.String("db", "data/teacher.db", "SQLite database path")
		flagNoMutex   = flag.Bool("no-single-instance", false, "Skip single-instance mutex check")
	)
	flag.Parse()

	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		log.Fatalf("failed to initialize platform runtime: %v", err)
	}

	var releaseMutex func()
	if !*flagNoMutex && rt.OS == platform.Windows {
		releaseMutex, err = winplat.EnsureSingleInstance(defaultTeacherMutex)
		if err != nil {
			log.Fatalf("teacher console startup rejected: %v", err)
		}
		defer releaseMutex()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	app, err := NewTeacherApp(ctx, rt, *flagDB, *flagHTTP, *flagDiscovery, releaseMutex)
	if err != nil {
		log.Fatalf("failed to create teacher app: %v", err)
	}

	if err := app.Start(); err != nil {
		log.Fatalf("failed to start teacher app: %v", err)
	}

	fmt.Printf("\n=======================================================\n")
	fmt.Printf(" LocalMesh 教师端控制台 (极域复刻版) 已就绪!\n")
	fmt.Printf(" 访问控制台界面: http://%s\n", *flagHTTP)
	fmt.Printf("=======================================================\n\n")

	<-ctx.Done()
	log.Println("Shutting down LocalMesh Teacher Console...")
	app.Stop()
	log.Println("LocalMesh Teacher Console stopped.")
}

func NewTeacherApp(ctx context.Context, rt platform.Runtime, dbPath, httpAddr, udpAddr string, releaseMutex func()) (*TeacherApp, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}

	discReg := discovery.NewRegistry()

	var udpSrv *udpdiscovery.Server
	if udpAddr != "" {
		uaddr, err := net.ResolveUDPAddr("udp", udpAddr)
		if err == nil {
			srv, err := udpdiscovery.Listen(uaddr, discReg)
			if err == nil {
				udpSrv = srv
			} else {
				log.Printf("Warning: UDP discovery listener failed to bind %s: %v", udpAddr, err)
			}
		}
	}

	chatMgr := interaction.NewChatManager()
	raiseHandQ := interaction.NewRaiseHandQueue()
	seating, _ := interaction.NewSeatingChart(defaultClassroomID, "MainLab", 8, 8)
	thumbs := screen.NewThumbnailCache(320, 180, 10*time.Second)
	lockMgr := screenlock.NewManager(screenlock.NewPlatformTrapper())
	powerSvc := power.NewService(&power.OSCommandExecutor{OS: string(rt.OS)}, nil)
	procMon := restrictions.NewProcessMonitor(nil, nil)

	var nfAdapter netfilter.FirewallAdapter
	if rt.OS == platform.Windows {
		nfAdapter = netfilter.NewWindowsNetshAdapter()
	} else {
		nfAdapter = netfilter.NewMockFirewallAdapter()
	}
	nfMgr := netfilter.NewManager(nfAdapter)

	coll := inventory.NewDefaultCollector()
	invMgr, _ := inventory.NewManager(coll)

	apiSrv := NewAPIServer(discReg, chatMgr, raiseHandQ, seating, thumbs, powerSvc, lockMgr, procMon, nfMgr, invMgr)

	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: apiSrv.Handler(),
	}

	return &TeacherApp{
		platformRT:   rt,
		store:        store,
		discoveryReg: discReg,
		udpServer:    udpSrv,
		apiServer:    apiSrv,
		httpServer:   httpSrv,
		releaseMutex: releaseMutex,
	}, nil
}

func (a *TeacherApp) Start() error {
	if a.udpServer != nil {
		go func() {
			ctx := context.Background()
			_ = a.udpServer.Serve(ctx)
		}()
	}

	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return nil
}

func (a *TeacherApp) Stop() {
	if a.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.httpServer.Shutdown(shutdownCtx)
	}

	if a.udpServer != nil {
		_ = a.udpServer.Close()
	}

	if a.store != nil {
		_ = a.store.Close()
	}

	if a.releaseMutex != nil {
		a.releaseMutex()
	}
}
