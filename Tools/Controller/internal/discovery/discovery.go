// Package discovery advertises and discovers PCController host bridges using
// standard mDNS/DNS-SD and SSDP. It never exposes a service by itself; the IPC
// listener's separate authentication policy remains authoritative.
package discovery

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"

	"pccontroller.local/controller/internal/productidentity"
)

const (
	MDNSService = "_pccontroller._tcp"
	MDNSDomain  = "local."
	// SSDPType must exactly match the serviceType in /upnp/device.xml.
	SSDPType               = "urn:pccontroller-org:service:Controller:1"
	WSDiscoveryType        = "pc:PCControllerBridge"
	BroadcastPort          = 37889
	NetBIOSNameServicePort = 137
	ssdpAddress            = "239.255.255.250:1900"
	wsDiscoveryAddress     = "239.255.255.250:3702"
)

type Instance struct {
	Protocol  string      `json:"protocol"`
	Protocols []string    `json:"protocols,omitempty"`
	Sources   []Source    `json:"sources,omitempty"`
	Name      string      `json:"name"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Addresses []string    `json:"addresses,omitempty"`
	TXT       []string    `json:"txt,omitempty"`
	Location  string      `json:"location,omitempty"`
	PublicURL string      `json:"public_url,omitempty"`
	Public    *PublicInfo `json:"public,omitempty"`
	USN       string      `json:"usn,omitempty"`
	SeenAt    time.Time   `json:"seen_at"`
}

// Options selects the network discovery protocols. MDNS and DNSSD are the
// same DNS-SD transport; DNSSD is retained as an explicit API name so callers
// can describe their intent without losing compatibility with mDNS networks.
type Options struct {
	MDNS          bool
	DNSSD         bool
	SSDP          bool
	UPnP          bool
	WSDiscovery   bool
	Broadcast     bool
	NetBIOS       bool
	BroadcastPort int
}

func (options Options) normalized() Options {
	options.MDNS = options.MDNS || options.DNSSD
	options.SSDP = options.SSDP || options.UPnP
	if options.BroadcastPort == 0 {
		options.BroadcastPort = BroadcastPort
	}
	return options
}

type Advertiser struct {
	cancel           context.CancelFunc
	done             chan struct{}
	ssdpRefresh      chan struct{}
	broadcastRefresh chan struct{}
	name             string
	port             int
	ssdp             bool
	wsDiscovery      bool
	broadcast        bool
	broadcastPort    int
	netbios          bool
	ssdpListener     *net.UDPConn
	wsListener       *net.UDPConn
	broadcastConn    *net.UDPConn
	netbiosConn      *net.UDPConn

	mu       sync.RWMutex
	closed   bool
	mdns     *zeroconf.Server
	txt      []string
	active   []string
	failures []TransportFailure
}

// TransportFailure reports a configured discovery responder that could not
// start. Other transports remain available, so callers can expose degraded
// discovery without incorrectly claiming every configured responder is live.
type TransportFailure struct {
	Protocol string `json:"protocol"`
	Error    string `json:"error"`
}

func Advertise(
	parent context.Context,
	name string,
	port int,
	mdnsEnabled, ssdpEnabled bool,
	txt []string,
) (*Advertiser, error) {
	return AdvertiseWithOptions(parent, name, port, Options{MDNS: mdnsEnabled, SSDP: ssdpEnabled}, txt)
}

func AdvertiseWithOptions(
	parent context.Context,
	name string,
	port int,
	options Options,
	txt []string,
) (*Advertiser, error) {
	options = options.normalized()
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("discovery port %d is invalid", port)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = productidentity.DefaultAppTitle()
	}
	txt = normalizeTXT(txt)
	ctx, cancel := context.WithCancel(parent)
	result := &Advertiser{
		cancel: cancel, done: make(chan struct{}), ssdpRefresh: make(chan struct{}, 1), broadcastRefresh: make(chan struct{}, 1),
		name: name, port: port, ssdp: options.SSDP, wsDiscovery: options.WSDiscovery,
		broadcast: options.Broadcast, broadcastPort: options.BroadcastPort,
		netbios: options.NetBIOS, txt: append([]string(nil), txt...),
	}
	enabled := 0
	recordFailure := func(protocol string, err error) {
		if err != nil {
			result.failures = append(result.failures, TransportFailure{Protocol: protocol, Error: err.Error()})
		}
	}
	if options.MDNS {
		enabled++
		server, err := zeroconf.Register(name, MDNSService, MDNSDomain, port, txt, nil)
		if err != nil {
			recordFailure("dns-sd", err)
		} else {
			result.mdns = server
			result.active = append(result.active, "dns-sd")
		}
	}
	if options.SSDP {
		enabled++
		group, err := net.ResolveUDPAddr("udp4", ssdpAddress)
		if err == nil {
			result.ssdpListener, err = net.ListenMulticastUDP("udp4", nil, group)
		}
		if err != nil {
			recordFailure("ssdp-responder", err)
		}
		// NOTIFY remains useful even where the OS owns multicast port 1900.
		result.active = append(result.active, "ssdp")
	}
	if options.WSDiscovery {
		enabled++
		group, err := net.ResolveUDPAddr("udp4", wsDiscoveryAddress)
		if err == nil {
			result.wsListener, err = net.ListenMulticastUDP("udp4", nil, group)
		}
		if err != nil {
			recordFailure("ws-discovery", err)
			result.wsDiscovery = false
		} else {
			result.active = append(result.active, "ws-discovery")
		}
	}
	if options.Broadcast {
		enabled++
		connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: options.BroadcastPort})
		if err != nil {
			recordFailure("broadcast", err)
			result.broadcast = false
		} else {
			result.broadcastConn = connection
			result.active = append(result.active, "broadcast")
		}
	}
	if options.NetBIOS {
		enabled++
		connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: NetBIOSNameServicePort})
		if err != nil {
			recordFailure("netbios", err)
			result.netbios = false
		} else {
			result.netbiosConn = connection
			result.active = append(result.active, "netbios")
		}
	}
	if enabled > 0 && len(result.active) == 0 {
		cancel()
		messages := make([]error, 0, len(result.failures))
		for _, failure := range result.failures {
			messages = append(messages, fmt.Errorf("%s: %s", failure.Protocol, failure.Error))
		}
		return nil, errors.Join(messages...)
	}
	go func() {
		defer close(result.done)
		var wait sync.WaitGroup
		if result.ssdp {
			wait.Add(1)
			go func() { defer wait.Done(); runSSDPAdvertiser(ctx, result) }()
		}
		if result.wsDiscovery {
			wait.Add(1)
			go func() { defer wait.Done(); runWSDiscoveryAdvertiser(ctx, result) }()
		}
		if result.broadcast {
			wait.Add(1)
			go func() { defer wait.Done(); runBroadcastAdvertiser(ctx, result) }()
		}
		if result.netbios {
			wait.Add(1)
			go func() { defer wait.Done(); runNetBIOSAdvertiser(ctx, result) }()
		}
		wait.Wait()
	}()
	return result, nil
}

// ActiveProtocols returns the responders/advertisers that actually started.
func (advertiser *Advertiser) ActiveProtocols() []string {
	if advertiser == nil {
		return nil
	}
	advertiser.mu.RLock()
	defer advertiser.mu.RUnlock()
	return append([]string(nil), advertiser.active...)
}

// Failures returns configured transports that could not bind or register.
func (advertiser *Advertiser) Failures() []TransportFailure {
	if advertiser == nil {
		return nil
	}
	advertiser.mu.RLock()
	defer advertiser.mu.RUnlock()
	return append([]TransportFailure(nil), advertiser.failures...)
}

func (advertiser *Advertiser) Close() {
	if advertiser == nil {
		return
	}
	advertiser.mu.Lock()
	if advertiser.closed {
		advertiser.mu.Unlock()
		return
	}
	advertiser.closed = true
	mdns := advertiser.mdns
	advertiser.mdns = nil
	advertiser.mu.Unlock()
	advertiser.cancel()
	if mdns != nil {
		mdns.Shutdown()
	}
	select {
	case <-advertiser.done:
	case <-time.After(time.Second):
	}
}

// UpdateText publishes a changed, bounded set of non-secret discovery values.
// mDNS announces the new TXT record immediately, SSDP sends one changed-only
// alive packet, and UDP broadcast sends one changed-only beacon; callers
// therefore update from pushed state rather than polling.
func (advertiser *Advertiser) UpdateText(txt []string) {
	if advertiser == nil {
		return
	}
	txt = normalizeTXT(txt)
	advertiser.mu.Lock()
	if advertiser.closed || equalText(advertiser.txt, txt) {
		advertiser.mu.Unlock()
		return
	}
	advertiser.txt = append(advertiser.txt[:0], txt...)
	mdns := advertiser.mdns
	ssdp := advertiser.ssdp
	broadcast := advertiser.broadcast
	advertiser.mu.Unlock()
	if mdns != nil {
		mdns.SetText(txt)
	}
	if ssdp {
		select {
		case advertiser.ssdpRefresh <- struct{}{}:
		default:
		}
	}
	if broadcast {
		select {
		case advertiser.broadcastRefresh <- struct{}{}:
		default:
		}
	}
}

func (advertiser *Advertiser) text() []string {
	advertiser.mu.RLock()
	defer advertiser.mu.RUnlock()
	return append([]string(nil), advertiser.txt...)
}

func equalText(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeTXT(values []string) []string {
	// Sixty-four records retain the complete live status block after host and
	// board identity fields while remaining bounded for every wire adapter.
	const maximumRecords = 64
	result := make([]string, 0, min(len(values), maximumRecords))
	for _, raw := range values {
		value := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(raw))
		if value == "" {
			continue
		}
		key := value
		if index := strings.IndexByte(key, '='); index >= 0 {
			key = key[:index]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if discoverySecretKey(key) {
			continue
		}
		if len(value) > 255 {
			value = value[:255]
		}
		result = append(result, value)
		if len(result) == maximumRecords {
			break
		}
	}
	return result
}

func discoverySecretKey(key string) bool {
	for _, fragment := range []string{"authorization", "credential", "password", "secret", "token"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func Discover(ctx context.Context, useMDNS, useSSDP bool) ([]Instance, error) {
	return DiscoverWithOptions(ctx, Options{MDNS: useMDNS, SSDP: useSSDP})
}

func DiscoverWithOptions(ctx context.Context, options Options) ([]Instance, error) {
	options = options.normalized()
	var mutex sync.Mutex
	instances := make(map[string]Instance)
	seen := make(map[string]bool)
	enrichSlots := make(chan struct{}, 8)
	var enrich sync.WaitGroup
	add := func(instance Instance) {
		rawKey := instance.Protocol + "|" + instance.USN + "|" + instance.Host +
			"|" + strconv.Itoa(instance.Port)
		mutex.Lock()
		if seen[rawKey] {
			mutex.Unlock()
			return
		}
		seen[rawKey] = true
		mutex.Unlock()
		select {
		case enrichSlots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		enrich.Add(1)
		go func() {
			defer enrich.Done()
			defer func() { <-enrichSlots }()
			instance = enrichInstance(ctx, instance)
			key := instance.Protocol + "|" + instance.USN + "|" + instance.Host +
				"|" + strconv.Itoa(instance.Port)
			mutex.Lock()
			instances[key] = instance
			mutex.Unlock()
		}()
	}
	var wait sync.WaitGroup
	var firstError error
	var errorMu sync.Mutex
	recordError := func(err error) {
		if err == nil {
			return
		}
		errorMu.Lock()
		if firstError == nil {
			firstError = err
		}
		errorMu.Unlock()
	}
	if options.MDNS {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recordError(browseMDNS(ctx, add))
		}()
	}
	if options.SSDP {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recordError(discoverSSDP(ctx, add))
		}()
	}
	if options.WSDiscovery {
		wait.Add(1)
		go func() { defer wait.Done(); recordError(discoverWSDiscovery(ctx, add)) }()
	}
	if options.Broadcast {
		wait.Add(1)
		go func() { defer wait.Done(); recordError(discoverBroadcast(ctx, options.BroadcastPort, add)) }()
	}
	if options.NetBIOS {
		wait.Add(1)
		go func() { defer wait.Done(); recordError(discoverNetBIOS(ctx, add)) }()
	}
	wait.Wait()
	enrich.Wait()
	result := make([]Instance, 0, len(instances))
	for _, instance := range instances {
		result = append(result, instance)
	}
	result = mergeInstances(result)
	if len(result) == 0 && firstError != nil {
		return nil, firstError
	}
	return result, nil
}

func browseMDNS(ctx context.Context, add func(Instance)) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("create mDNS resolver: %w", err)
	}
	entries := make(chan *zeroconf.ServiceEntry)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for entry := range entries {
			addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
			for _, address := range entry.AddrIPv4 {
				addresses = append(addresses, address.String())
			}
			for _, address := range entry.AddrIPv6 {
				addresses = append(addresses, address.String())
			}
			add(Instance{
				Protocol: "mdns", Name: entry.Instance,
				Host: strings.TrimSuffix(entry.HostName, "."), Port: entry.Port,
				Addresses: addresses, TXT: append([]string(nil), entry.Text...),
				SeenAt: time.Now(),
			})
		}
	}()
	err = resolver.Browse(ctx, MDNSService, MDNSDomain, entries)
	if err != nil {
		close(entries)
		<-done
		return fmt.Errorf("browse mDNS: %w", err)
	}
	<-ctx.Done()
	<-done
	return nil
}

func runSSDPAdvertiser(ctx context.Context, advertiser *Advertiser) {
	listener := advertiser.ssdpListener
	if listener != nil {
		_ = listener.SetReadBuffer(64 * 1024)
		go func() {
			<-ctx.Done()
			_ = listener.Close()
		}()
		go respondSSDP(ctx, listener, advertiser)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	sendSSDPNotify(advertiser, "ssdp:alive")
	defer sendSSDPNotify(advertiser, "ssdp:byebye")
	for {
		select {
		case <-ctx.Done():
			return
		case <-advertiser.ssdpRefresh:
			sendSSDPNotify(advertiser, "ssdp:alive")
		case <-ticker.C:
			sendSSDPNotify(advertiser, "ssdp:alive")
		}
	}
}

func respondSSDP(
	ctx context.Context,
	connection *net.UDPConn,
	advertiser *Advertiser,
) {
	buffer := make([]byte, 8192)
	for ctx.Err() == nil {
		count, source, err := connection.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		request := strings.ToUpper(string(buffer[:count]))
		if !strings.HasPrefix(request, "M-SEARCH ") ||
			(!strings.Contains(request, "ST: "+strings.ToUpper(SSDPType)) &&
				!strings.Contains(request, "ST: SSDP:ALL")) {
			continue
		}
		name, port := advertiser.name, advertiser.port
		locationHost := localAddressFor(source)
		lines := []string{
			"HTTP/1.1 200 OK",
			"CACHE-CONTROL: max-age=60",
			"EXT:",
			"LOCATION: http://" + net.JoinHostPort(locationHost, strconv.Itoa(port)) + "/upnp/device.xml",
			"X-PCController-Public: http://" + net.JoinHostPort(locationHost, strconv.Itoa(port)) + PublicInfoPath,
			"SERVER: " + productidentity.ProtocolToken() + "/1 UPnP/1.1",
			"ST: " + SSDPType,
			"USN: " + ssdpUSNWithText(name, port, advertiser.text()) + "::" + SSDPType,
			"X-PCController-Name: " + sanitizeHeader(name),
		}
		lines = append(lines, ssdpMetadataHeaders(advertiser.text())...)
		lines = append(lines, "", "")
		response := strings.Join(lines, "\r\n")
		_, _ = connection.WriteToUDP([]byte(response), source)
	}
}

func localAddressFor(remote *net.UDPAddr) string {
	connection, err := net.DialUDP("udp4", nil, remote)
	if err == nil {
		defer connection.Close()
		if local, ok := connection.LocalAddr().(*net.UDPAddr); ok && local.IP != nil {
			return local.IP.String()
		}
	}
	return "127.0.0.1"
}

func sendSSDPNotify(advertiser *Advertiser, nts string) {
	address, err := net.ResolveUDPAddr("udp4", ssdpAddress)
	if err != nil {
		return
	}
	connection, err := net.DialUDP("udp4", nil, address)
	if err != nil {
		return
	}
	defer connection.Close()
	_, _ = connection.Write([]byte(ssdpNotifyWithText(
		advertiser.name, advertiser.port, nts, advertiser.text(),
	)))
}

func ssdpNotify(name string, port int, nts string) string {
	return ssdpNotifyWithText(name, port, nts, nil)
}

func ssdpNotifyWithText(name string, port int, nts string, txt []string) string {
	usn := ssdpUSNWithText(name, port, txt)
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "127.0.0.1"
	}
	lines := []string{
		"NOTIFY * HTTP/1.1",
		"HOST: " + ssdpAddress,
		"CACHE-CONTROL: max-age=60",
		"LOCATION: http://" + net.JoinHostPort(hostname, strconv.Itoa(port)) + "/upnp/device.xml",
		"X-PCController-Public: http://" + net.JoinHostPort(hostname, strconv.Itoa(port)) + PublicInfoPath,
		"NT: " + SSDPType,
		"NTS: " + nts,
		"SERVER: " + productidentity.ProtocolToken() + "/1 UPnP/1.1",
		"USN: " + usn + "::" + SSDPType,
		"X-PCController-Name: " + sanitizeHeader(name),
	}
	lines = append(lines, ssdpMetadataHeaders(txt)...)
	lines = append(lines, "", "")
	return strings.Join(lines, "\r\n")
}

func ssdpMetadataHeaders(txt []string) []string {
	values := normalizeTXT(txt)
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, "X-PCController-Meta: "+sanitizeHeader(value))
	}
	return result
}

func ssdpUSN(name string, port int) string {
	return ssdpUSNWithText(name, port, nil)
}

func ssdpUSNWithText(name string, port int, txt []string) string {
	for _, value := range normalizeTXT(txt) {
		key, instanceID, ok := strings.Cut(value, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "instance.id") && strings.TrimSpace(instanceID) != "" {
			return "uuid:" + sanitizeHeader(strings.TrimSpace(instanceID))
		}
	}
	digest := sha256.Sum256([]byte(name + ":" + strconv.Itoa(port)))
	return fmt.Sprintf("uuid:%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

func discoverSSDP(ctx context.Context, add func(Instance)) error {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("listen for SSDP: %w", err)
	}
	defer connection.Close()
	address, err := net.ResolveUDPAddr("udp4", ssdpAddress)
	if err != nil {
		return err
	}
	request := strings.Join([]string{
		"M-SEARCH * HTTP/1.1", "HOST: " + ssdpAddress,
		`MAN: "ssdp:discover"`, "MX: 1", "ST: " + SSDPType, "", "",
	}, "\r\n")
	if _, err := connection.WriteToUDP([]byte(request), address); err != nil {
		return fmt.Errorf("send SSDP search: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()
	buffer := make([]byte, 8192)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		count, source, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if netError, ok := err.(net.Error); ok && netError.Timeout() {
				continue
			}
			return err
		}
		if instance, ok := parseSSDPResponse(buffer[:count], source); ok {
			add(instance)
		}
	}
}

func parseSSDPResponse(data []byte, source *net.UDPAddr) (Instance, bool) {
	request, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(data))), nil)
	if err != nil {
		return Instance{}, false
	}
	_ = request.Body.Close()
	if request.Header.Get("ST") != SSDPType {
		return Instance{}, false
	}
	host := ""
	addresses := []string{}
	if source != nil {
		host = source.IP.String()
		addresses = append(addresses, host)
	}
	port := 0
	if location := request.Header.Get("Location"); location != "" {
		if parsed, err := neturlParse(location); err == nil {
			if host == "" {
				host = parsed.host
			}
			port = parsed.port
		}
	}
	return Instance{
		Protocol: "ssdp", Name: request.Header.Get("X-PCController-Name"),
		Host: host, Port: port, Addresses: addresses, Location: request.Header.Get("Location"), PublicURL: request.Header.Get("X-PCController-Public"),
		USN: request.Header.Get("USN"),
		TXT: normalizeTXT(request.Header.Values("X-PCController-Meta")), SeenAt: time.Now(),
	}, true
}

type parsedLocation struct {
	host string
	port int
}

func neturlParse(value string) (parsedLocation, error) {
	request, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil {
		return parsedLocation{}, err
	}
	host := request.URL.Hostname()
	port := 80
	if request.URL.Scheme == "https" {
		port = 443
	}
	if raw := request.URL.Port(); raw != "" {
		port, err = strconv.Atoi(raw)
	}
	return parsedLocation{host: host, port: port}, err
}
