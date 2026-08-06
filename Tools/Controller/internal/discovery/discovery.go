// Package discovery advertises and discovers PCController host bridges using
// standard mDNS/DNS-SD and SSDP. It never exposes a service by itself; the IPC
// listener's separate authentication policy remains authoritative.
package discovery

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
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
	SSDPType    = "urn:pccontroller-org:service:bridge:1"
	ssdpAddress = "239.255.255.250:1900"
)

type Instance struct {
	Protocol  string    `json:"protocol"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Addresses []string  `json:"addresses,omitempty"`
	TXT       []string  `json:"txt,omitempty"`
	Location  string    `json:"location,omitempty"`
	USN       string    `json:"usn,omitempty"`
	SeenAt    time.Time `json:"seen_at"`
}

type Advertiser struct {
	cancel  context.CancelFunc
	done    chan struct{}
	refresh chan struct{}
	name    string
	port    int
	ssdp    bool

	mu     sync.RWMutex
	closed bool
	mdns   *zeroconf.Server
	txt    []string
}

func Advertise(
	parent context.Context,
	name string,
	port int,
	mdnsEnabled, ssdpEnabled bool,
	txt []string,
) (*Advertiser, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("discovery port %d is invalid", port)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = productidentity.DefaultTitle
	}
	txt = normalizeTXT(txt)
	ctx, cancel := context.WithCancel(parent)
	result := &Advertiser{
		cancel: cancel, done: make(chan struct{}), refresh: make(chan struct{}, 1),
		name: name, port: port, ssdp: ssdpEnabled, txt: append([]string(nil), txt...),
	}
	if mdnsEnabled {
		server, err := zeroconf.Register(name, MDNSService, MDNSDomain, port, txt, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("advertise mDNS: %w", err)
		}
		result.mdns = server
	}
	go func() {
		defer close(result.done)
		if ssdpEnabled {
			runSSDPAdvertiser(ctx, result)
			return
		}
		<-ctx.Done()
	}()
	return result, nil
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
// mDNS announces the new TXT record immediately and SSDP sends one changed-only
// alive packet; callers therefore update from pushed state rather than polling.
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
	advertiser.mu.Unlock()
	if mdns != nil {
		mdns.SetText(txt)
	}
	if ssdp {
		select {
		case advertiser.refresh <- struct{}{}:
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
	const maximumRecords = 48
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
	var mutex sync.Mutex
	instances := make(map[string]Instance)
	add := func(instance Instance) {
		key := instance.Protocol + "|" + instance.USN + "|" + instance.Host +
			"|" + strconv.Itoa(instance.Port)
		mutex.Lock()
		instances[key] = instance
		mutex.Unlock()
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
	if useMDNS {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recordError(browseMDNS(ctx, add))
		}()
	}
	if useSSDP {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recordError(discoverSSDP(ctx, add))
		}()
	}
	wait.Wait()
	result := make([]Instance, 0, len(instances))
	for _, instance := range instances {
		result = append(result, instance)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Protocol < result[j].Protocol
		}
		return result[i].Name < result[j].Name
	})
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
		return fmt.Errorf("browse mDNS: %w", err)
	}
	<-ctx.Done()
	<-done
	return nil
}

func runSSDPAdvertiser(ctx context.Context, advertiser *Advertiser) {
	group, _ := net.ResolveUDPAddr("udp4", ssdpAddress)
	listener, _ := net.ListenMulticastUDP("udp4", nil, group)
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
		case <-advertiser.refresh:
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
			"LOCATION: http://" + net.JoinHostPort(locationHost, strconv.Itoa(port)) + "/healthz",
			"SERVER: " + productidentity.ProtocolToken() + "/1 UPnP/1.1",
			"ST: " + SSDPType,
			"USN: " + ssdpUSN(name, port) + "::" + SSDPType,
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
	usn := ssdpUSN(name, port)
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "127.0.0.1"
	}
	lines := []string{
		"NOTIFY * HTTP/1.1",
		"HOST: " + ssdpAddress,
		"CACHE-CONTROL: max-age=60",
		"LOCATION: http://" + net.JoinHostPort(hostname, strconv.Itoa(port)) + "/healthz",
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
	if source != nil {
		host = source.IP.String()
	}
	port := 0
	if location := request.Header.Get("Location"); location != "" {
		if parsed, err := neturlParse(location); err == nil {
			host = parsed.host
			port = parsed.port
		}
	}
	return Instance{
		Protocol: "ssdp", Name: request.Header.Get("X-PCController-Name"),
		Host: host, Port: port, Location: request.Header.Get("Location"),
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
