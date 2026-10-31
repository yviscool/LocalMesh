package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"encoding/json"
	"localmesh/internal/capabilities/filetransfer"
	"localmesh/internal/capabilities/power"
	"localmesh/internal/capabilities/registry"
	"localmesh/internal/capabilities/restrictions"
	"localmesh/internal/capabilities/screenlock"
	"localmesh/internal/capabilities/watchdog"
	"localmesh/internal/capabilities/workers"
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
)

type AgentApp struct {
	deviceID      identity.DeviceID
	pubKey        ed25519.PublicKey
	privKey       ed25519.PrivateKey
	platformRT    platform.Runtime
	capRegistry   *registry.Registry
	supervisor    *workers.Supervisor
	lockMgr       *screenlock.Manager
	powerSvc      *power.Service
	processMon    *restrictions.ProcessMonitor
	fileRcvr      *filetransfer.Receiver
	ipcListener   ipc.Listener
	pipeAddress   string
	discoveryAddr string
	discoveryStop chan struct{}
	releaseMutex  func()
}

func main() {
	var (
		flagID                 = flag.String("id", "", "Stable device identifier (min 8 chars)")
		flagPipe               = flag.String("pipe", "", "IPC endpoint address (named pipe or unix socket)")
		flagDiscovery          = flag.Bool("discovery", true, "Enable UDP broadcast announcement")
		flagDiscoveryAddr      = flag.String("discovery-addr", defaultDiscoveryAddr, "UDP discovery broadcast address")
		flagInstallAutostart   = flag.Bool("install-autostart", false, "Configure agent to launch automatically on logon")
		flagUninstallAutostart = flag.Bool("uninstall-autostart", false, "Remove autostart logon configuration")
		flagNoMutex            = flag.Bool("no-single-instance", false, "Skip single-instance mutex check")
	)
	flag.Parse()

	// Autostart management flags
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

	// Platform runtime bootstrap
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		log.Fatalf("failed to initialize platform runtime: %v", err)
	}

	// Single instance enforcement
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

	pipeAddr := *flagPipe
	if pipeAddr == "" {
		if rt.OS == platform.Windows {
			pipeAddr = defaultWindowsPipe
		} else {
			pipeAddr = defaultUnixSocket
		}
	}

	app, err := NewAgentApp(rt, *flagID, pipeAddr, *flagDiscoveryAddr, releaseMutex)
	if err != nil {
		log.Fatalf("failed to initialize agent: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.Start(ctx, *flagDiscovery); err != nil {
		log.Fatalf("agent error: %v", err)
	}

	<-ctx.Done()
	log.Println("Shutting down LocalMesh Student Agent gracefully...")
	app.Stop()
	log.Println("LocalMesh Student Agent stopped.")
}

func NewAgentApp(rt platform.Runtime, customID string, pipeAddr, discoveryAddr string, releaseMutex func()) (*AgentApp, error) {
	// Stable device identity
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

	lockMgr := screenlock.NewManager(screenlock.NewPlatformTrapper())
	powerSvc := power.NewService(&power.OSCommandExecutor{OS: string(rt.OS)}, nil)
	procMon := restrictions.NewProcessMonitor(&simpleProcessController{}, nil)
	fileRcvr := filetransfer.NewReceiver()

	return &AgentApp{
		deviceID:      devID,
		pubKey:        pub,
		privKey:       priv,
		platformRT:    rt,
		capRegistry:   capReg,
		supervisor:    sup,
		lockMgr:       lockMgr,
		powerSvc:      powerSvc,
		processMon:    procMon,
		fileRcvr:      fileRcvr,
		pipeAddress:   pipeAddr,
		discoveryAddr: discoveryAddr,
		discoveryStop: make(chan struct{}),
		releaseMutex:  releaseMutex,
	}, nil
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

	// 2. Start UDP Discovery Broadcaster
	if enableDiscovery {
		go a.startDiscoveryAnnouncer(ctx)
	}

	return nil
}

func (a *AgentApp) Stop() {
	if a.discoveryStop != nil {
		select {
		case <-a.discoveryStop:
		default:
			close(a.discoveryStop)
		}
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
			var req ipc.Request
			if err := ipc.Read(c, &req, ipc.DefaultMaxMessage); err != nil {
				return
			}

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

	ann := udpdiscovery.Announcement{
		Version:      udpdiscovery.ProtocolVersion,
		DeviceID:     string(a.deviceID),
		Endpoint:     a.pipeAddress,
		Capabilities: caps,
		TTLSeconds:   30,
	}

	payload, err := json.Marshal(ann)
	if err != nil {
		return
	}

	log.Printf("UDP Discovery announcer broadcasting to %s", a.discoveryAddr)

	// Send initial burst
	_, _ = conn.Write(payload)

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.discoveryStop:
			return
		case <-ticker.C:
			_, _ = conn.Write(payload)
		}
	}
}

type simpleProcessController struct{}

func (s *simpleProcessController) ListProcesses(_ context.Context) ([]restrictions.ProcessInfo, error) {
	return []restrictions.ProcessInfo{}, nil
}

func (s *simpleProcessController) KillProcess(_ context.Context, _ int) error {
	return nil
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
