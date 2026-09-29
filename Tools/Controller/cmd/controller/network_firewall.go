package main

import "context"

type networkFirewallRule struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Action    string `json:"action"`
	Profile   string `json:"profile"`
	Protocol  string `json:"protocol"`
}

type networkFirewallReport struct {
	Executable     string                `json:"executable"`
	RemovedRules   bool                  `json:"removed_existing_rules"`
	CanonicalRules []networkFirewallRule `json:"canonical_rules"`
}

var ensureCanonicalNetworkFirewall = platformEnsureCanonicalNetworkFirewall

func firewallCommandOutput(ctx context.Context, runner func(context.Context, string, ...string) ([]byte, error), arguments ...string) error {
	_, err := runner(ctx, "netsh", arguments...)
	return err
}
