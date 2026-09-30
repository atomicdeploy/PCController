//go:build !windows

package main

import (
	"context"
	"errors"
)

func platformEnsureCanonicalNetworkFirewall(context.Context) (networkFirewallReport, error) {
	return networkFirewallReport{}, errors.New("network firewall provisioning is currently available only on Windows")
}
