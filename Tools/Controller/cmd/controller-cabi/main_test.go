//go:build controllerlib

package main

import (
	"errors"
	"testing"

	controller "pccontroller.local/controller"
)

func TestDestroyRetainsHandleWhenShutdownFails(t *testing.T) {
	client := controller.New(controller.Options{})
	handle := nextHandle.Add(1)
	clientsMu.Lock()
	clients[handle] = &libraryClient{client: client}
	clientsMu.Unlock()
	t.Cleanup(func() {
		_ = client.Shutdown()
		clientsMu.Lock()
		delete(clients, handle)
		clientsMu.Unlock()
	})

	closeErr := errors.New("cancel serial I/O")
	previous := shutdownClient
	attempts := 0
	shutdownClient = func(candidate *controller.Client) error {
		attempts++
		if attempts == 1 {
			return closeErr
		}
		return candidate.Shutdown()
	}
	defer func() { shutdownClient = previous }()

	failed := invoke(libraryRequest{Operation: "destroy", Handle: handle})
	if failed.OK || failed.Error == "" {
		t.Fatalf("failed destroy response = %#v", failed)
	}
	if getClient(handle) == nil {
		t.Fatal("failed destroy discarded the retryable client handle")
	}

	succeeded := invoke(libraryRequest{Operation: "destroy", Handle: handle})
	if !succeeded.OK || succeeded.Error != "" {
		t.Fatalf("retry destroy response = %#v", succeeded)
	}
	if getClient(handle) != nil {
		t.Fatal("successful destroy retained the client handle")
	}
}
