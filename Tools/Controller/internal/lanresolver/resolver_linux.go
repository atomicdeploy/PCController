//go:build linux

package lanresolver

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"strings"
	"time"
)

// lookupPlatform uses the operating system's NSS path first. That path is
// where Avahi/mDNS and configured WINS/NBNS support live on Linux. nmblookup
// is the explicit NetBIOS fallback for a single-label Windows host name.
func lookupPlatform(ctx context.Context, host string) ([]netip.Addr, error) {
	lookupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, getentErr := lookupCommand(lookupContext, "/usr/bin/getent", "ahostsv4", host)
	if len(addresses) != 0 {
		return addresses, nil
	}
	if strings.Contains(host, ".") {
		return nil, getentErr
	}
	addresses, nbnsErr := lookupCommand(lookupContext, "/usr/bin/nmblookup", host)
	if len(addresses) != 0 {
		return addresses, nil
	}
	return nil, errors.Join(getentErr, nbnsErr)
}

func lookupCommand(ctx context.Context, command string, args ...string) ([]netip.Addr, error) {
	output, err := exec.CommandContext(ctx, command, args...).Output()
	addresses := parseAddresses(string(output))
	if len(addresses) != 0 {
		return addresses, nil
	}
	return nil, err
}

func parseAddresses(output string) []netip.Addr {
	var result []netip.Addr
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if address, err := netip.ParseAddr(fields[0]); err == nil {
			result = append(result, address.Unmap())
		}
	}
	return unique(result)
}
