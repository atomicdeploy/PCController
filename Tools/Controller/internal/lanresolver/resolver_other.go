//go:build !linux

package lanresolver

import (
	"context"
	"net/netip"
)

func lookupPlatform(context.Context, string) ([]netip.Addr, error) { return nil, nil }
