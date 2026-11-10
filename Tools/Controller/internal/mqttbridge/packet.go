//go:build !no_mqtt

package mqttbridge

import (
	"errors"
	"io"
	"net"
)

// Bound the broker-controlled Remaining Length before Paho allocates a packet.
// One MQTT reader uses this connection; writes and deadlines remain delegated.
type packetConn struct {
	net.Conn
	header                 [5]byte
	offset, end, remaining int
}

func (c *packetConn) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	if c.offset < c.end {
		n := copy(out, c.header[c.offset:c.end])
		c.offset += n
		return n, nil
	}
	if c.remaining > 0 {
		n, err := c.Conn.Read(out[:min(len(out), c.remaining)])
		c.remaining -= n
		return n, err
	}
	if _, err := io.ReadFull(c.Conn, c.header[:1]); err != nil {
		return 0, err
	}
	for i := 1; i <= 4; i++ {
		if _, err := io.ReadFull(c.Conn, c.header[i:i+1]); err != nil {
			return 0, err
		}
		value := c.header[i]
		c.remaining += int(value&127) << (7 * (i - 1))
		if c.remaining > maxCommandBytes+512 {
			break
		}
		if value&128 == 0 {
			c.offset, c.end = 0, i+1
			return c.Read(out)
		}
	}
	_ = c.Conn.Close()
	return 0, errors.New("MQTT broker packet exceeds receive limit or has invalid length")
}
