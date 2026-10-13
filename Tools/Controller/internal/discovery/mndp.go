package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const MNDPPort = 5678
const MNDPProductTLV = 0xff01
const MNDPAPIPortTLV = 0xff02

var mndpSequence atomic.Uint32

// Standard MNDP has no application API port. The two Digitalogic extension
// TLVs identify a service/port, never credentials or permission. Generic RouterOS
// neighbours without these extensions are not invented as HTTP port 80 peers.
func EncodeMNDP(name, product, identity string, port int, seq uint16) ([]byte, error) {
	if len(name) == 0 || len(name) > 96 || !utf8.ValidString(name) || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || len(identity) > 128 || port < 1 || port > 65535 || !knownProduct(product) {
		return nil, errors.New("invalid MNDP identity")
	}
	b := []byte{0, 0, byte(seq >> 8), byte(seq)}
	add := func(kind uint16, value []byte) {
		b = binary.BigEndian.AppendUint16(b, kind)
		b = binary.BigEndian.AppendUint16(b, uint16(len(value)))
		b = append(b, value...)
	}
	// A locally administered application-scoped identity, not a fabricated
	// physical NIC or board MAC. Software-ID/interface describe that scope.
	hash := sha256.Sum256([]byte(product + ":" + identity))
	mac := append([]byte(nil), hash[:6]...)
	mac[0] = (mac[0] | 2) & 0xfe
	add(1, mac)
	add(5, []byte(name))
	add(8, []byte("Digitalogic"))
	add(11, []byte(identity))
	add(12, []byte("Application endpoint"))
	add(16, []byte("application"))
	add(MNDPProductTLV, []byte(product))
	add(MNDPAPIPortTLV, []byte{byte(port >> 8), byte(port)})
	return b, nil
}
func knownProduct(product string) bool {
	return product == "pccontroller" || product == "pealayer" || product == "controller-mobile"
}
func ParseMNDP(b []byte, source net.IP) (Instance, error) {
	bad := func() (Instance, error) { return Instance{}, errors.New("invalid or unsupported MNDP packet") }
	if len(b) < 14 || len(b) > 2048 || b[0] != 0 || b[1] != 0 || source == nil || source.To4() == nil || source.IsUnspecified() || source.IsMulticast() {
		return bad()
	}
	fields := map[uint16][]byte{}
	for i := 4; i < len(b); {
		if i+4 > len(b) {
			return bad()
		}
		kind, n := binary.BigEndian.Uint16(b[i:i+2]), int(binary.BigEndian.Uint16(b[i+2:i+4]))
		i += 4
		if i+n > len(b) || fields[kind] != nil {
			return bad()
		}
		fields[kind] = b[i : i+n]
		i += n
	}
	name, product := string(fields[5]), string(fields[MNDPProductTLV])
	portBytes := fields[MNDPAPIPortTLV]
	if len(fields[1]) != 6 || len(name) == 0 || len(name) > 96 || !utf8.ValidString(name) || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 || !knownProduct(product) || len(portBytes) != 2 {
		return bad()
	}
	port := int(binary.BigEndian.Uint16(portBytes))
	if port == 0 {
		return bad()
	}
	return Instance{Protocol: "mndp", Product: product, Name: name, Host: source.String(), Port: port, Addresses: []string{source.String()}, TXT: []string{"product=" + product, "api_port=" + strconv.Itoa(port)}, USN: string(fields[11]), SeenAt: time.Now()}, nil
}
func runMNDPAdvertiser(ctx context.Context, a *Advertiser) {
	c := a.mndpConn
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	last := time.Time{}
	replies := map[string]time.Time{}
	b := make([]byte, 2049)
	announce := func(to *net.UDPAddr) {
		b, err := EncodeMNDP(a.name, "pccontroller", ssdpUSNWithText(a.name, a.port, a.text()), a.port, uint16(mndpSequence.Add(1)))
		if err == nil {
			c.WriteToUDP(b, to)
		}
	}
	for ctx.Err() == nil {
		if time.Since(last) >= 30*time.Second {
			last = time.Now()
			announce(&net.UDPAddr{IP: net.IPv4bcast, Port: MNDPPort})
			announce(&net.UDPAddr{IP: net.IPv4(224, 0, 0, 88), Port: MNDPPort})
		}
		c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, from, err := c.ReadFromUDP(b)
		if err == nil && n == 4 && string(b[:4]) == "\x00\x00\x00\x00" {
			for ip, at := range replies {
				if time.Since(at) >= time.Second {
					delete(replies, ip)
				}
			}
			ip := from.IP.String()
			if _, exists := replies[ip]; exists || len(replies) >= 128 {
				continue
			}
			replies[ip] = time.Now()
			announce(from)
		} else if err != nil {
			if e, ok := err.(net.Error); !ok || !e.Timeout() {
				return
			}
		}
	}
}
func discoverMNDP(ctx context.Context, add func(Instance)) error {
	group, _ := net.ResolveUDPAddr("udp4", "224.0.0.88:5678")
	c, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return err
	}
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	c.WriteToUDP([]byte{0, 0, 0, 0}, &net.UDPAddr{IP: net.IPv4bcast, Port: MNDPPort})
	b := make([]byte, 2049)
	for ctx.Err() == nil {
		c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, from, err := c.ReadFromUDP(b)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if peer, err := ParseMNDP(b[:n], from.IP); err == nil {
			add(peer)
		}
	}
	return nil
}
