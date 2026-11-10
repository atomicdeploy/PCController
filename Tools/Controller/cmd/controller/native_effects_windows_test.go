//go:build windows && !no_native_gui

package main

import (
	"context"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/control"
)

func TestNativeEffectCreatePreservesUnappliedAdvancedDraft(t *testing.T) {
	for _, draft := range []string{`{"name":"Unsaved"}`, `{invalid literal draft`} {
		p := nativePages{rpc: func(_ context.Context, method string, _, result any) error {
			if method != "controller.snapshot" {
				t.Fatalf("dirty draft invoked mutation %s", method)
			}
			*result.(*control.Snapshot) = control.Snapshot{}
			return nil
		}}
		values := map[string]string{"id": "new", "name": "New", "kind": "sequence", "definition": draft, "definition_baseline": `{}`}
		_, err := p.submitEffectsPage(context.Background(), "create", values)
		if err == nil || !strings.Contains(err.Error(), "unapplied") || values["definition"] != draft {
			t.Fatalf("advanced draft lost: %q, %v", values["definition"], err)
		}
	}
}
