package hostui

import (
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
