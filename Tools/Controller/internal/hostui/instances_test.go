package hostui

import (
	"net/url"
	"testing"
	"time"
)

func TestInstanceRegistryReportsQueriesAndExpiresSurfaces(t *testing.T) {
	registry := NewInstanceRegistry()
	now := time.Date(2026, 8, 3, 20, 0, 0, 0, time.UTC)
	registry.now = func() time.Time { return now }
	changes := make([]InstanceChange, 0, 2)
	registry.SetObserver(func(change InstanceChange) { changes = append(changes, change) })

	web, err := registry.Upsert(AppInstance{
		ID: "web:tab-1", Surface: "webui", Page: "controls", State: "active",
		LeaseSeconds: 45, Values: map[string]string{"theme": "dark"},
		Self: &InstanceSelf{
			Kind: "browser", Vars: map[string]string{"platform": "Win32"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if web.RegisteredAt != now || web.ExpiresAt != now.Add(45*time.Second) ||
		len(changes) != 1 || changes[0].Kind != "joined" {
		t.Fatalf("web instance=%#v changes=%#v", web, changes)
	}
	web.Values["theme"] = "mutated"
	web.Self.Vars["platform"] = "mutated"
	stored, ok := registry.Get("web:tab-1")
	if !ok || stored.Values["theme"] != "dark" || stored.Self == nil ||
		stored.Self.Vars["platform"] != "Win32" {
		t.Fatalf("registry leaked caller mutation: %#v", stored)
	}

	now = now.Add(46 * time.Second)
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("expired browser instance remained: %#v", got)
	}
}

func TestInstanceRegistryRejectsCredentialLikeSelfVars(t *testing.T) {
	registry := NewInstanceRegistry()
	_, err := registry.Upsert(AppInstance{
		ID: "web:tab-1", Surface: "webui",
		Self: &InstanceSelf{
			Kind: "browser", Vars: map[string]string{"session_token": "do-not-store"},
		},
	})
	if err == nil {
		t.Fatal("credential-like instance self var was accepted")
	}
}

func TestInstanceRegistryRejectsCredentialLikeValues(t *testing.T) {
	registry := NewInstanceRegistry()
	_, err := registry.Upsert(AppInstance{
		ID: "web:tab-1", Surface: "webui",
		Values: map[string]string{"access_token": "do-not-store"},
	})
	if err == nil {
		t.Fatal("credential-like instance value was accepted")
	}
}

func TestInstanceRegistryPreservesPealayerUnifiedControlEndpoints(t *testing.T) {
	registry := NewInstanceRegistry()
	instance, err := registry.Upsert(AppInstance{
		ID: "pealayer:desktop", Surface: "pealayer", State: "active",
		Self: &InstanceSelf{Kind: "native", Vars: map[string]string{
			"rpc":       "http://127.0.0.1:8080/api/rpc",
			"websocket": "ws://127.0.0.1:8080/ws",
			"ipc":       "http://127.0.0.1:8080/api/ipc",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if instance.Self == nil {
		t.Fatal("Pealayer process metadata was discarded")
	}
	for name, expectedPath := range map[string]string{
		"rpc": "/api/rpc", "websocket": "/ws", "ipc": "/api/ipc",
	} {
		endpoint, parseErr := url.Parse(instance.Self.Vars[name])
		if parseErr != nil || endpoint.Host != "127.0.0.1:8080" || endpoint.Path != expectedPath {
			t.Fatalf("%s endpoint=%q parseErr=%v", name, instance.Self.Vars[name], parseErr)
		}
	}
}
