//go:build linux

package lanresolver

import "testing"

func TestParseAddressesAcceptsNSSAndNetBIOSOutput(t *testing.T) {
	values := parseAddresses("192.168.100.155 STREAM cafe-pc.local\nquerying CAFE-PC on 192.168.100.255\n192.168.100.155 CAFE-PC<00>\n")
	if len(values) != 1 || values[0].String() != "192.168.100.155" {
		t.Fatalf("%v", values)
	}
}
