package udp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/ipv4"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Enabled         bool              `json:"enabled"`
	Listen          string            `json:"listen"`
	Discovery       bool              `json:"discovery"`
	BroadcastEvents bool              `json:"broadcast_events"`
	Peers           map[string]string `json:"peers,omitempty"`
}

func (c Config) Validate() error {
	a, e := literalAddress(c.Listen)
	if (c.Enabled || c.Listen != "") && (e != nil || a == nil || a.Port == 0 || a.IP == nil) {
		return errors.New("UDP listen must be a literal IPv4 address and nonzero port")
	}
	if c.Enabled && c.Discovery && a != nil && a.IP.IsLoopback() {
		return errors.New("UDP discovery requires a LAN or wildcard listening address")
	}
	if c.BroadcastEvents && !c.Discovery {
		return errors.New("UDP event broadcast requires discovery")
	}
	if len(c.Peers) > 64 {
		return errors.New("too many UDP peers")
	}
	for name, addr := range c.Peers {
		a, e := literalAddress(addr)
		if name == "" || len(name) > 128 || e != nil || a == nil || a.Port == 0 || a.IP == nil || a.IP.IsUnspecified() || a.IP.IsMulticast() || a.IP.Equal(net.IPv4bcast) {
			return errors.New("UDP peers require names and literal unicast IPv4 endpoints")
		}
	}
	return nil
}
func literalAddress(address string) (*net.UDPAddr, error) {
	a, err := netip.ParseAddrPort(address)
	if err != nil || !a.Addr().Is4() {
		return nil, errors.New("literal IPv4 endpoint required")
	}
	return net.UDPAddrFromAddrPort(a), nil
}

type Peer struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Identity any       `json:"identity"`
	SeenAt   time.Time `json:"seen_at"`
}
type ReceivedEvent struct {
	Source     string    `json:"source"`
	Sequence   uint64    `json:"sequence"`
	Value      any       `json:"value"`
	ReceivedAt time.Time `json:"received_at"`
}
type fragmentKey struct {
	Address string
	Source  [16]byte
	ID      uint64
	Kind    byte
}
type assembly struct {
	Parts [][]byte
	At    time.Time
}
type receipt struct {
	High uint64
	Bits uint64
}
type subscription struct {
	Address *net.UDPAddr
	Until   time.Time
}
type pendingCall struct {
	Address string
	Result  chan any
}
type Endpoint struct {
	conn          *net.UDPConn
	discovery     *net.UDPConn
	config        Config
	source        [16]byte
	sequence      atomic.Uint64
	eventSequence atomic.Uint64
	ctx           context.Context
	cancel        context.CancelFunc
	handle        func(context.Context, byte, any, *net.UDPAddr) any
	identity      any
	mu            sync.Mutex
	eventMu       sync.Mutex
	peers         map[string]Peer
	events        []ReceivedEvent
	pending       map[uint64]pendingCall
	subs          map[string]subscription
	assemblies    map[fragmentKey]*assembly
	receipts      map[string]*receipt
	lastEvents    map[string]uint64
	workers       chan struct{}
}

func Start(ctx context.Context, config Config, identity any, handle func(context.Context, byte, any, *net.UDPAddr) any) (*Endpoint, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	a, err := net.ResolveUDPAddr("udp4", config.Listen)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp4", a)
	if err != nil {
		return nil, err
	}
	return startSocket(ctx, config, identity, handle, c)
}

// StartClient owns a reusable ephemeral socket; it never advertises or accepts
// commands. Retain it for a session instead of opening a new sender per call.
func StartClient(ctx context.Context, address string) (*Endpoint, error) {
	config := Config{Listen: "127.0.0.1:1", Peers: map[string]string{"remote": address}}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	remote, _ := net.ResolveUDPAddr("udp4", address)
	ip := net.IPv4zero
	if remote.IP.IsLoopback() {
		ip = net.IPv4(127, 0, 0, 1)
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	if err != nil {
		return nil, err
	}
	return startSocket(ctx, config, map[string]any{"app": "controller-client"}, func(context.Context, byte, any, *net.UDPAddr) any {
		return map[string]any{"error": "client endpoint does not accept commands"}
	}, c)
}

func startSocket(parent context.Context, config Config, identity any, handle func(context.Context, byte, any, *net.UDPAddr) any, c *net.UDPConn) (*Endpoint, error) {
	var err error
	ctx := parent
	ctx, cancel := context.WithCancel(ctx)
	e := &Endpoint{conn: c, config: config, ctx: ctx, cancel: cancel, identity: identity, handle: handle, peers: map[string]Peer{}, pending: map[uint64]pendingCall{}, subs: map[string]subscription{}, assemblies: map[fragmentKey]*assembly{}, receipts: map[string]*receipt{}, lastEvents: map[string]uint64{}, workers: make(chan struct{}, 16)}
	if _, err = rand.Read(e.source[:]); err != nil {
		c.Close()
		cancel()
		return nil, err
	}
	if config.Discovery {
		group, _ := net.ResolveUDPAddr("udp4", DiscoveryAddress)
		iface := interfaceForIP(c.LocalAddr().(*net.UDPAddr).IP)
		if iface != nil {
			if err = ipv4.NewPacketConn(c).SetMulticastInterface(iface); err != nil {
				c.Close()
				cancel()
				return nil, err
			}
		}
		e.discovery, err = net.ListenMulticastUDP("udp4", iface, group)
		if err != nil {
			c.Close()
			cancel()
			return nil, fmt.Errorf("UDP discovery: %w", err)
		}
		go e.read(e.discovery)
	}
	go e.read(c)
	go func() {
		<-ctx.Done()
		c.Close()
		if e.discovery != nil {
			e.discovery.Close()
		}
	}()
	go e.beacons()
	return e, nil
}
func interfaceForIP(ip net.IP) *net.Interface {
	if ip.IsUnspecified() {
		return nil
	}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			network, _, err := net.ParseCIDR(address.String())
			if err == nil && network.Equal(ip) {
				copy := iface
				return &copy
			}
		}
	}
	return nil
}
func (e *Endpoint) Close() {
	e.cancel()
	_ = e.conn.Close()
	if e.discovery != nil {
		_ = e.discovery.Close()
	}
}
func (e *Endpoint) Address() string { return e.conn.LocalAddr().String() }
func (e *Endpoint) WantsEvents() bool {
	if e.config.BroadcastEvents && e.config.Discovery {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, sub := range e.subs {
		if time.Now().Before(sub.Until) {
			return true
		}
	}
	return false
}
func (e *Endpoint) Status() any {
	e.mu.Lock()
	defer e.mu.Unlock()
	peers := []Peer{}
	for _, p := range e.peers {
		if time.Since(p.SeenAt) < 45*time.Second {
			peers = append(peers, p)
		}
	}
	return map[string]any{"running": e.ctx.Err() == nil, "listen": e.Address(), "id": hex.EncodeToString(e.source[:]), "peers": peers, "received_events": len(e.events)}
}

// Received returns a bounded page. UDP consumers explicitly refresh the
// authoritative peer after gaps; these observations are never board commands.
func (e *Endpoint) Received() any {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := []ReceivedEvent{}
	for i := len(e.events) - 1; i >= 0; i-- {
		next := append([]ReceivedEvent{e.events[i]}, result...)
		b, err := Marshal(next)
		if err != nil || len(b) > 128*1024 {
			break
		}
		result = next
	}
	return result
}
func (e *Endpoint) send(kind byte, id uint64, value any, to *net.UDPAddr) error {
	b, err := Marshal(value)
	if err != nil {
		return err
	}
	count := (len(b) + ChunkSize - 1) / ChunkSize
	if count == 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		end := min((i+1)*ChunkSize, len(b))
		wire, er := EncodePacket(Packet{Kind: kind, Source: e.source, ID: id, Index: uint16(i), Count: uint16(count), Data: b[i*ChunkSize : end]})
		if er != nil {
			return er
		}
		if _, er = e.conn.WriteToUDP(wire, to); er != nil {
			return er
		}
	}
	return nil
}
func (e *Endpoint) Call(ctx context.Context, target string, kind byte, value any) (any, error) {
	addr, ok := e.config.Peers[target]
	if !ok {
		return nil, errors.New("UDP target is not provisioned")
	}
	a, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	if kind != Discover {
		announcement, err := e.Call(ctx, target, Discover, map[string]any{})
		if err != nil {
			return nil, err
		}
		metadata, ok := announcement.(map[string]any)
		if !ok {
			return nil, errors.New("invalid UDP identity")
		}
		value = map[string]any{"target_id": metadata["id"], "body": value}
	}
	id := e.sequence.Add(1)
	result := make(chan any, 1)
	e.mu.Lock()
	if len(e.pending) >= 64 {
		e.mu.Unlock()
		return nil, errors.New("UDP pending calls full")
	}
	e.pending[id] = pendingCall{a.String(), result}
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.pending, id); e.mu.Unlock() }()
	if err = e.send(kind, id, value, a); err != nil {
		return nil, err
	}
	select {
	case v := <-result:
		return v, nil
	case <-ctx.Done():
		return nil, errors.New("UDP outcome unknown: request is not retried")
	case <-e.ctx.Done():
		return nil, errors.New("UDP stopped; outcome unknown")
	}
}
func (e *Endpoint) Publish(value any) {
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	value = map[string]any{"sequence": e.eventSequence.Add(1), "event": value}
	e.mu.Lock()
	targets := []*net.UDPAddr{}
	for k, s := range e.subs {
		if time.Now().Before(s.Until) {
			targets = append(targets, s.Address)
		} else {
			delete(e.subs, k)
		}
	}
	e.mu.Unlock()
	id := e.sequence.Add(1)
	for _, a := range targets {
		_ = e.send(Event, id, value, a)
	}
	if e.config.BroadcastEvents && e.config.Discovery {
		a, _ := net.ResolveUDPAddr("udp4", DiscoveryAddress)
		_ = e.send(Event, id, value, a)
	}
}
func (e *Endpoint) announcement() any {
	return map[string]any{"id": hex.EncodeToString(e.source[:]), "identity": e.identity, "port": e.conn.LocalAddr().(*net.UDPAddr).Port, "transport": "digitalogic-binary-udp", "commands": "unicast", "max_message": MaxMessage}
}
func (e *Endpoint) beacons() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	announce := func() {
		if e.config.Discovery {
			a, _ := net.ResolveUDPAddr("udp4", DiscoveryAddress)
			_ = e.send(Announce, e.sequence.Add(1), e.announcement(), a)
			a.IP = net.IPv4bcast
			_ = e.send(Announce, e.sequence.Add(1), e.announcement(), a)
		}
	}
	announce()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-t.C:
			announce()
			e.mu.Lock()
			for k, a := range e.assemblies {
				if time.Since(a.At) > 3*time.Second {
					delete(e.assemblies, k)
				}
			}
			for k, p := range e.peers {
				if time.Since(p.SeenAt) > 45*time.Second {
					delete(e.peers, k)
				}
			}
			e.mu.Unlock()
		}
	}
}
func (e *Endpoint) read(c *net.UDPConn) {
	buffer := make([]byte, 1101)
	for {
		n, from, err := c.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		p, err := DecodePacket(buffer[:n])
		if err != nil || p.Source == e.source {
			continue
		}
		// Discovery socket must never accept actuating request/subscription traffic.
		if c == e.discovery && p.Kind != Discover && p.Kind != Announce && p.Kind != Event {
			continue
		}
		value, ok := e.reassemble(p, from)
		if !ok {
			continue
		}
		switch p.Kind {
		case Discover:
			_ = e.send(Announce, p.ID, e.announcement(), from)
		case Announce:
			v, ok := value.(map[string]any)
			if !ok {
				continue
			}
			number, ok := v["port"].(json.Number)
			port, err := strconv.Atoi(string(number))
			if !ok || err != nil || port < 1 || port > 65535 {
				continue
			}
			id := hex.EncodeToString(p.Source[:])
			if v["id"] != id {
				continue
			}
			e.mu.Lock()
			if call, ok := e.pending[p.ID]; ok && call.Address == from.String() {
				select {
				case call.Result <- value:
				default:
				}
			}
			if len(e.peers) < 64 || e.peers[id].ID != "" {
				e.peers[id] = Peer{id, net.JoinHostPort(from.IP.String(), fmt.Sprint(int(port))), v["identity"], time.Now()}
			}
			e.mu.Unlock()
		case Response:
			e.mu.Lock()
			call, ok := e.pending[p.ID]
			e.mu.Unlock()
			if ok && call.Address == from.String() {
				select {
				case call.Result <- value:
				default:
				}
			}
		case Event:
			envelope, ok := value.(map[string]any)
			if !ok {
				continue
			}
			number, ok := envelope["sequence"].(json.Number)
			if !ok {
				continue
			}
			seq, err := strconv.ParseUint(string(number), 10, 64)
			if err != nil || seq == 0 {
				continue
			}
			source := hex.EncodeToString(p.Source[:])
			e.mu.Lock()
			previous := e.lastEvents[source]
			if seq <= previous || (previous == 0 && len(e.lastEvents) >= 256) {
				e.mu.Unlock()
				continue
			}
			e.lastEvents[source] = seq
			if len(e.events) >= 64 {
				e.events = e.events[1:]
			}
			e.events = append(e.events, ReceivedEvent{source, seq, map[string]any{"event": envelope["event"], "refresh_required": previous == 0 || seq != previous+1}, time.Now()})
			e.mu.Unlock()
		case Request, Subscribe, Unsubscribe:
			wrapped, ok := value.(map[string]any)
			if !ok || wrapped["target_id"] != hex.EncodeToString(e.source[:]) {
				continue
			}
			value = wrapped["body"]
			if !e.claim(p, from) {
				continue
			}
			select {
			case e.workers <- struct{}{}:
			default:
				_ = e.send(Response, p.ID, map[string]any{"error": map[string]any{"code": -32000, "message": "UDP dispatcher busy; request not executed"}}, from)
				continue
			}
			go func(p Packet, value any, from *net.UDPAddr) {
				defer func() { <-e.workers }()
				ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
				defer cancel()
				result := e.handle(ctx, p.Kind, value, from)
				if p.Kind == Subscribe {
					if m, ok := result.(map[string]any); ok && m["subscribed"] == true {
						e.mu.Lock()
						if len(e.subs) < 64 || e.subs[from.String()].Address != nil {
							e.subs[from.String()] = subscription{from, time.Now().Add(30 * time.Second)}
						} else {
							result = map[string]any{"error": map[string]any{"code": -32000, "message": "UDP subscriptions full"}}
						}
						e.mu.Unlock()
					}
				}
				if p.Kind == Unsubscribe {
					e.mu.Lock()
					delete(e.subs, from.String())
					e.mu.Unlock()
				}
				if err := e.send(Response, p.ID, result, from); err != nil {
					_ = e.send(Response, p.ID, map[string]any{"error": map[string]any{"code": -32010, "message": err.Error()}}, from)
				}
			}(p, value, from)
		}
	}
}
func (e *Endpoint) claim(p Packet, from *net.UDPAddr) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := from.String() + hex.EncodeToString(p.Source[:])
	r := e.receipts[key]
	if r == nil {
		if len(e.receipts) >= 256 {
			return false
		}
		r = &receipt{}
		e.receipts[key] = r
	}
	if p.ID > r.High {
		delta := p.ID - r.High
		if delta >= 64 {
			r.Bits = 0
		} else {
			r.Bits <<= delta
		}
		r.High = p.ID
		r.Bits |= 1
		return true
	}
	delta := r.High - p.ID
	if delta >= 64 || r.Bits&(uint64(1)<<delta) != 0 {
		return false
	}
	r.Bits |= uint64(1) << delta
	return true
}
func (e *Endpoint) reassemble(p Packet, from *net.UDPAddr) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := fragmentKey{from.String(), p.Source, p.ID, p.Kind}
	a := e.assemblies[key]
	if a != nil && time.Since(a.At) > 3*time.Second {
		delete(e.assemblies, key)
		a = nil
	}
	if a == nil {
		for k, old := range e.assemblies {
			if time.Since(old.At) > 3*time.Second {
				delete(e.assemblies, k)
			}
		}
		if len(e.assemblies) >= 16 {
			return nil, false
		}
		a = &assembly{make([][]byte, p.Count), time.Now()}
		e.assemblies[key] = a
	}
	if len(a.Parts) != int(p.Count) {
		delete(e.assemblies, key)
		return nil, false
	}
	if a.Parts[p.Index] != nil {
		if string(a.Parts[p.Index]) != string(p.Data) {
			delete(e.assemblies, key)
		}
		return nil, false
	}
	a.Parts[p.Index] = p.Data
	var size int
	for _, b := range a.Parts {
		if b == nil {
			return nil, false
		}
		size += len(b)
	}
	if size > MaxMessage {
		delete(e.assemblies, key)
		return nil, false
	}
	b := make([]byte, 0, size)
	for _, part := range a.Parts {
		b = append(b, part...)
	}
	delete(e.assemblies, key)
	var value any
	if Unmarshal(b, &value) != nil {
		return nil, false
	}
	return value, true
}
