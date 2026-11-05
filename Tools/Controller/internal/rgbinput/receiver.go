package rgbinput

import (
	"context"
	"errors"
	"fmt"
	"go.bug.st/serial"
	"net"
	"sync"
	"time"
)

type Config struct {
	Serial   string        `json:"serial,omitempty"`
	Baud     int           `json:"baud,omitempty"`
	Protocol string        `json:"protocol"`
	Listen   string        `json:"listen"`
	Source   string        `json:"source,omitempty"`
	Pixels   int           `json:"pixels"`
	FPS      int           `json:"fps"`
	Timeout  time.Duration `json:"timeout_ns"`
	Universe int           `json:"universe,omitempty"`
	Channel  int           `json:"channel,omitempty"`
}

type State struct {
	Config
	Running   bool      `json:"running"`
	Sender    string    `json:"sender,omitempty"`
	Received  uint64    `json:"received"`
	Rejected  uint64    `json:"rejected"`
	Coalesced uint64    `json:"coalesced"`
	Applied   uint64    `json:"applied"`
	LastFrame time.Time `json:"last_frame,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

type Receiver struct {
	serialPort   serial.Port
	framer       *streamFramer
	serialBuffer []byte
	serialOffset int
	closeOnce    sync.Once
	closeErr     error
	conn         *net.UDPConn
	allowed      net.IP
	mu           sync.Mutex
	state        State
}

func Open(cfg Config) (*Receiver, error) {
	if !supported(cfg.Protocol) {
		return nil, errors.New("unsupported RGB input protocol")
	}
	if cfg.Pixels < 1 || cfg.Pixels > 100 || cfg.FPS < 1 || cfg.FPS > 120 || cfg.Timeout < time.Second || cfg.Timeout > time.Minute {
		return nil, errors.New("RGB input requires 1..100 pixels, 1..120 FPS and a 1s..1m timeout")
	}
	if cfg.Channel == 0 {
		cfg.Channel = 1
	}
	if cfg.Protocol == "artnet" || cfg.Protocol == "sacn" {
		maxUniverse, minUniverse := 32767, 0
		if cfg.Protocol == "sacn" {
			maxUniverse, minUniverse = 63999, 1
		}
		if cfg.Universe < minUniverse || cfg.Universe > maxUniverse || cfg.Channel < 1 || cfg.Channel > 513-cfg.Pixels*3 {
			return nil, errors.New("invalid universe or RGB window exceeds 512 DMX slots")
		}
	}
	if cfg.Serial != "" {
		port, err := openSerial(cfg)
		if err != nil {
			return nil, err
		}
		return &Receiver{serialPort: port, framer: newStreamFramer(cfg.Protocol, cfg.Pixels), state: State{Config: cfg, Running: true}}, nil
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, errors.New("listen must use a specific local IP address, not a hostname or wildcard")
	}
	allowed := net.ParseIP(cfg.Source)
	if cfg.Source != "" && (allowed == nil || allowed.IsUnspecified() || allowed.IsMulticast()) {
		return nil, errors.New("source must be one unicast IP address")
	}
	if !ip.IsLoopback() && allowed == nil {
		return nil, errors.New("LAN input requires --source with the trusted sender's IP")
	}
	address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip.String(), port))
	if err != nil {
		return nil, err
	}
	if address.Port == 0 {
		return nil, errors.New("listen port must be 1..65535")
	}
	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		return nil, err
	}
	return &Receiver{conn: conn, allowed: allowed, state: State{Config: cfg, Running: true}}, nil
}

func (r *Receiver) Close() error {
	r.closeOnce.Do(func() {
		if r.serialPort != nil {
			r.closeErr = r.serialPort.Close()
		} else {
			r.closeErr = r.conn.Close()
		}
	})
	return r.closeErr
}
func (r *Receiver) State() State { r.mu.Lock(); defer r.mu.Unlock(); return r.state }

type frame struct {
	rgb      []byte
	received time.Time
}

// Run has one network reader and one ACK-paced writer. The mailbox is latest-
// frame-only; a slow serial transport cannot accumulate stale animation frames.
// Check runs while idle as well, fencing disconnect, E-stop and board replacement.
func (r *Receiver) Run(ctx context.Context, check func() error, apply func(context.Context, []byte) error) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.state.Running = true
	r.mu.Unlock()
	defer func() {
		cancel()
		_ = r.Close()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.state.Running = false
		if result != nil && !errors.Is(result, context.Canceled) {
			r.state.LastError = result.Error()
		}
	}()
	mailbox := make(chan frame, 1)
	readErr := make(chan error, 1)
	readerDone := make(chan struct{})
	stopClose := context.AfterFunc(ctx, func() { _ = r.Close() })
	defer stopClose()
	go func() {
		defer close(readerDone)
		// Read the entire maximum UDP datagram so Windows WSAEMSGSIZE cannot
		// turn one oversized/untrusted packet into a fatal socket-read error.
		// Decode still admits only the configured <=100-pixel frame.
		buffer := make([]byte, 65536)
		owner := ""
		order := packetOrder{}
		for {
			n, peerName, permitted, err := r.readPacket(ctx, buffer)
			if err != nil {
				readErr <- err
				return
			}
			if !permitted || (owner != "" && owner != peerName) {
				r.mu.Lock()
				r.state.Rejected++
				r.mu.Unlock()
				continue
			}
			packet, parseErr := decodePacket(r.state.Config, buffer[:n])
			if parseErr != nil || (packet.Ended && owner == "") || !order.accept(packet) {
				r.mu.Lock()
				r.state.Rejected++
				r.mu.Unlock()
				continue
			}
			if packet.Ended {
				readErr <- errors.New("sACN source ended the stream; last acknowledged colors retained")
				return
			}
			owner = peerName
			now := time.Now()
			value := frame{rgb: append([]byte(nil), packet.RGB...), received: now}
			r.mu.Lock()
			r.state.Received++
			r.state.Sender = owner
			r.state.LastFrame = now
			r.mu.Unlock()
			select {
			case mailbox <- value:
			default:
				select {
				case <-mailbox:
					r.mu.Lock()
					r.state.Coalesced++
					r.mu.Unlock()
				default:
				}
				select {
				case mailbox <- value:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	defer func() { cancel(); _ = r.Close(); <-readerDone }()
	tick := time.NewTicker(time.Second / time.Duration(r.state.FPS))
	defer tick.Stop()
	guard := time.NewTicker(100 * time.Millisecond)
	defer guard.Stop()
	started := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("RGB input: %w", err)
		case <-guard.C:
			if err := check(); err != nil {
				return err
			}
			state := r.State()
			if state.LastFrame.IsZero() {
				if time.Since(started) > time.Minute {
					return errors.New("RGB input received no valid frame for 60s; start it again when the sender is ready")
				}
			} else if time.Since(state.LastFrame) > state.Timeout {
				return errors.New("RGB sender timed out; last acknowledged colors retained")
			}
		case <-tick.C:
			select {
			case value := <-mailbox:
				if time.Since(value.received) > r.state.Timeout {
					continue
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := check(); err != nil {
					return err
				}
				if err := apply(ctx, value.rgb); err != nil {
					return fmt.Errorf("RGB frame not acknowledged; input stopped: %w", err)
				}
				r.mu.Lock()
				r.state.Applied++
				r.mu.Unlock()
			default:
			}
		}
	}
}
