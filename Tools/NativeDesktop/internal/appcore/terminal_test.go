package appcore

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/lamp"
)

func TestDeviceTerminalUsesConfiguredLampAndSharesOrderedBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = connection.Write([]byte{
			0xff, 0xfb, 0x01, // firmware's IAC WILL ECHO
		})
		_, _ = connection.Write([]byte("\x1b[2JWelcome to Rayan Lamp\r\nShell is on Serial\r\n"))
		var one [1]byte
		_, _ = connection.Read(one[:])
	}()

	cfg := config.Default()
	cfg.Lamp.BaseURL = "http://127.0.0.1"
	cfg.Lamp.TelnetPort = port
	cfg.Lamp.WebSocket = false
	cfg.Lamp.HTTPPoll = 0
	cfg.Lamp.RequestTimeout = config.Duration(2 * time.Second)
	core, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connected, err := core.TerminalConnect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if connected.State != deviceterminal.StateConnected ||
		connected.Host != "127.0.0.1" ||
		connected.Port != port {
		t.Fatalf("connected snapshot = %#v", connected)
	}

	var transcript strings.Builder
	var result deviceterminal.ReadResult
	after := uint64(0)
	for !strings.Contains(transcript.String(), "Welcome to Rayan Lamp") {
		result, err = core.TerminalRead(ctx, after, 0, 2*time.Second)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for _, chunk := range result.Chunks {
			if chunk.Sequence > after {
				after = chunk.Sequence
			}
			data, decodeErr := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
			if decodeErr != nil {
				t.Fatalf("decode output: %v", decodeErr)
			}
			transcript.Write(data)
		}
	}
	if strings.ContainsRune(transcript.String(), rune(0xff)) {
		t.Fatalf("Telnet negotiation leaked into transcript: %q", transcript.String())
	}
	if !strings.Contains(transcript.String(), "Welcome to Rayan Lamp") {
		t.Fatalf("transcript = %q", transcript.String())
	}
	if !result.Snapshot.ReadOnly {
		t.Fatalf("ShellOnSerial banner did not mark terminal read-only: %#v", result.Snapshot)
	}

	if _, err := core.TerminalWrite(ctx, []byte("status\r\n")); !errors.Is(
		err,
		deviceterminal.ErrReadOnly,
	) {
		t.Fatalf("write error = %v, want ErrReadOnly", err)
	}

	updated := cfg
	updated.Lamp.TelnetPort = port + 1
	if updated.Lamp.TelnetPort > 65535 {
		updated.Lamp.TelnetPort = port - 1
	}
	if err := core.ApplyConfig(updated); err != nil {
		t.Fatalf("apply config: %v", err)
	}
	if status := core.TerminalStatus(); status.State != deviceterminal.StateDisconnected {
		t.Fatalf("terminal remained connected after target change: %#v", status)
	}
}

func TestApplyConfigSerializesTerminalConnectTarget(t *testing.T) {
	cfg := config.Default()
	cfg.Lamp.BaseURL = "http://old-rayanlamp.invalid"
	cfg.Lamp.TelnetPort = 23001
	cfg.Lamp.WebSocket = false
	cfg.Lamp.HTTPPoll = 0
	cfg.Lamp.RequestTimeout = config.Duration(2 * time.Second)
	core, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dialed := make(chan int, 2)
	peers := make(chan net.Conn, 2)
	core.terminalDial = func(
		_ context.Context,
		_ *lamp.Client,
		port int,
	) (net.Conn, error) {
		connection, peer := net.Pipe()
		peers <- peer
		dialed <- port
		return connection, nil
	}
	defer func() {
		core.Stop()
		for {
			select {
			case peer := <-peers:
				_ = peer.Close()
			default:
				return
			}
		}
	}()

	// Give ApplyConfig an existing network generation whose shutdown can be
	// paused deterministically. At applyPaused it owns lifecycleOpMu, has
	// selected the old generation for shutdown, and has not committed the new
	// lamp target yet.
	networkContext, cancelNetwork := context.WithCancel(context.Background())
	run := &networkRun{ctx: networkContext, cancel: cancelNetwork}
	applyPaused := make(chan struct{})
	releaseApply := make(chan struct{})
	run.wg.Add(1)
	go func() {
		defer run.wg.Done()
		<-networkContext.Done()
		close(applyPaused)
		<-releaseApply
	}()
	core.lifecycleMu.Lock()
	core.network = run
	core.lifecycleMu.Unlock()

	updated := cfg
	updated.Lamp.BaseURL = "http://new-rayanlamp.invalid"
	updated.Lamp.TelnetPort = 23002
	applyDone := make(chan error, 1)
	go func() {
		applyDone <- core.ApplyConfig(updated)
	}()
	select {
	case <-applyPaused:
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyConfig did not reach the controlled lifecycle barrier")
	}

	type connectResult struct {
		snapshot deviceterminal.Snapshot
		err      error
	}
	connectStarted := make(chan struct{})
	connectDone := make(chan connectResult, 1)
	go func() {
		close(connectStarted)
		snapshot, connectErr := core.TerminalConnect(context.Background())
		connectDone <- connectResult{snapshot: snapshot, err: connectErr}
	}()
	<-connectStarted

	// TerminalConnect must be waiting at the lifecycle boundary. Without that
	// serialization it reaches the injected dialer with the old port while
	// ApplyConfig is paused, recreating the stale-target race.
	earlyPort := 0
	select {
	case earlyPort = <-dialed:
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseApply)
	if err := <-applyDone; err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	connected := <-connectDone
	if connected.err != nil {
		t.Fatalf("TerminalConnect: %v", connected.err)
	}
	dialPort := earlyPort
	if dialPort == 0 {
		select {
		case dialPort = <-dialed:
		case <-time.After(2 * time.Second):
			t.Fatal("TerminalConnect never reached the replacement target")
		}
	}
	if earlyPort != 0 {
		t.Errorf(
			"terminal dialed port %d before ApplyConfig committed the replacement",
			earlyPort,
		)
	}
	if dialPort != updated.Lamp.TelnetPort {
		t.Errorf(
			"terminal dialed stale port %d, want %d",
			dialPort,
			updated.Lamp.TelnetPort,
		)
	}
	if connected.snapshot.State != deviceterminal.StateConnected ||
		connected.snapshot.Host != "new-rayanlamp.invalid" ||
		connected.snapshot.Port != updated.Lamp.TelnetPort {
		t.Fatalf("connected replacement snapshot = %#v", connected.snapshot)
	}
	if status := core.TerminalStatus(); status.State != deviceterminal.StateConnected ||
		status.Host != connected.snapshot.Host ||
		status.Port != connected.snapshot.Port {
		t.Fatalf("terminal status mislabeled live connection: %#v", status)
	}
}

func TestTerminalLifecycleCancelsBlockedDialPromptly(t *testing.T) {
	tests := []struct {
		name   string
		action func(*Core, config.Config) error
	}{
		{
			name: "apply config",
			action: func(core *Core, cfg config.Config) error {
				cfg.Lamp.BaseURL = "http://replacement-rayanlamp.invalid"
				cfg.Lamp.TelnetPort++
				return core.ApplyConfig(cfg)
			},
		},
		{
			name: "stop",
			action: func(core *Core, _ config.Config) error {
				core.Stop()
				return nil
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Lamp.BaseURL = "http://blocked-rayanlamp.invalid"
			cfg.Lamp.TelnetPort = 24001
			cfg.Lamp.WebSocket = false
			cfg.Lamp.HTTPPoll = 0
			// A coarse lifecycle lock would make the action wait for all 30
			// seconds. The injected dial exits only when appcore cancels it.
			cfg.Lamp.RequestTimeout = config.Duration(30 * time.Second)
			core, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer core.Stop()

			dialEntered := make(chan struct{}, 1)
			core.terminalDial = func(
				ctx context.Context,
				_ *lamp.Client,
				_ int,
			) (net.Conn, error) {
				dialEntered <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}

			type connectResult struct {
				snapshot deviceterminal.Snapshot
				err      error
			}
			connectDone := make(chan connectResult, 1)
			go func() {
				snapshot, connectErr := core.TerminalConnect(context.Background())
				connectDone <- connectResult{snapshot: snapshot, err: connectErr}
			}()
			select {
			case <-dialEntered:
			case <-time.After(2 * time.Second):
				t.Fatal("blocked terminal dial did not start")
			}

			actionDone := make(chan error, 1)
			started := time.Now()
			go func() {
				actionDone <- test.action(core, cfg)
			}()
			select {
			case actionErr := <-actionDone:
				if actionErr != nil {
					t.Fatalf("lifecycle action: %v", actionErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("lifecycle action stalled behind the blocked terminal dial")
			}
			if elapsed := time.Since(started); elapsed >= 2*time.Second {
				t.Errorf("lifecycle cancellation took %s", elapsed)
			}

			var connected connectResult
			select {
			case connected = <-connectDone:
			case <-time.After(2 * time.Second):
				t.Fatal("canceled TerminalConnect did not join promptly")
			}
			if !errors.Is(connected.err, context.Canceled) {
				t.Fatalf(
					"TerminalConnect error = %v, want context.Canceled",
					connected.err,
				)
			}
			if status := core.TerminalStatus(); status.State !=
				deviceterminal.StateDisconnected {
				t.Fatalf("old terminal target survived lifecycle action: %#v", status)
			}
		})
	}
}

func TestDecorateTerminalSnapshotPreservesLiveTarget(t *testing.T) {
	cfg := config.Default()
	cfg.Lamp.BaseURL = "http://configured-rayanlamp.invalid"
	cfg.Lamp.TelnetPort = 23002
	cfg.Lamp.WebSocket = false
	cfg.Lamp.HTTPPoll = 0
	core, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop()

	live := deviceterminal.Snapshot{
		State: deviceterminal.StateConnected,
		Host:  "actually-connected.invalid",
		Port:  23001,
	}
	decorated := core.decorateTerminalSnapshot(live)
	if decorated.Host != live.Host || decorated.Port != live.Port {
		t.Fatalf("live target was hidden by configured metadata: %#v", decorated)
	}

	disconnected := core.decorateTerminalSnapshot(deviceterminal.Snapshot{
		State: deviceterminal.StateDisconnected,
		Host:  live.Host,
		Port:  live.Port,
	})
	if disconnected.Host != "configured-rayanlamp.invalid" ||
		disconnected.Port != cfg.Lamp.TelnetPort {
		t.Fatalf("disconnected target was not decorated from config: %#v", disconnected)
	}
}
