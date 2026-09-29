//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testManagedBrowserSession struct {
	closed bool
}

func (*testManagedBrowserSession) Wait(context.Context) error { return nil }
func (session *testManagedBrowserSession) Close(context.Context) error {
	session.closed = true
	return nil
}

func TestManagedBrowserTargetMustMatchConfiguredListener(t *testing.T) {
	for _, test := range []struct {
		target string
		listen string
		ok     bool
	}{
		{target: "http://127.0.0.1:8787/", listen: "0.0.0.0:8787", ok: true},
		{target: "http://[::1]:8787/#/dashboard", listen: "[::]:8787", ok: true},
		{target: "http://127.0.0.1:8788/", listen: "0.0.0.0:8787"},
		{target: "http://server:8787/", listen: "0.0.0.0:8787"},
		{target: "http://127.0.0.1:8787/?token=bad", listen: "0.0.0.0:8787"},
	} {
		if err := validateManagedTarget(test.target, test.listen); (err == nil) != test.ok {
			t.Fatalf("target=%q listen=%q err=%v want-ok=%v", test.target, test.listen, err, test.ok)
		}
	}
}

func TestManagedBrowserChildEnvironmentExcludesSecret(t *testing.T) {
	const secret = "vault-token-never-in-child-environment"
	filtered := environmentWithoutSecret([]string{
		"DISPLAY=:0", "PCC_TOKEN=" + secret, "WRAPPED=prefix-" + secret + "-suffix",
	}, secret)
	joined := strings.Join(filtered, "\n")
	if strings.Contains(joined, secret) || !strings.Contains(joined, "DISPLAY=:0") {
		t.Fatalf("filtered environment=%q", joined)
	}
}

func TestNotifyFailureClosesManagedBrowserSession(t *testing.T) {
	session := &testManagedBrowserSession{}
	err := announceManagedBrowserReady(session, func() error { return errors.New("synthetic notify failure") })
	if err == nil || !strings.Contains(err.Error(), "synthetic notify failure") || !session.closed {
		t.Fatalf("notify failure err=%v closed=%v", err, session.closed)
	}
}
