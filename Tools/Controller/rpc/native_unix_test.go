//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package rpc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnixEndpointRoundTripPermissionsAndCleanup(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := Endpoint{Transport: TransportUnix, Address: filepath.Join(parent, "controller.sock")}
	listener, err := Listen(endpoint, ListenOptions{RecoverStaleNative: true})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(endpoint.Address)
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket info=%v err=%v", info, err)
	}
	done := make(chan error, 1)
	go echoOne(listener, done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Call(ctx, endpoint, Request{Method: "controller.ping"}, ClientOptions{})
	if err != nil || response.Error != nil {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(endpoint.Address); !os.IsNotExist(err) {
		t.Fatalf("socket was not removed: %v", err)
	}
}

func TestUnixEndpointRefusesUnsafeAndLivePaths(t *testing.T) {
	unsafeParent := t.TempDir()
	if err := os.Chmod(unsafeParent, 0o755); err != nil {
		t.Fatal(err)
	}
	unsafe := Endpoint{Transport: TransportUnix, Address: filepath.Join(unsafeParent, "controller.sock")}
	if listener, err := Listen(unsafe, ListenOptions{RecoverStaleNative: true}); err == nil {
		listener.Close()
		t.Fatal("group-readable socket parent was accepted")
	}

	safeParent := t.TempDir()
	if err := os.Chmod(safeParent, 0o700); err != nil {
		t.Fatal(err)
	}
	live := Endpoint{Transport: TransportUnix, Address: filepath.Join(safeParent, "live.sock")}
	listener, err := Listen(live, ListenOptions{RecoverStaleNative: true})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if second, err := Listen(live, ListenOptions{RecoverStaleNative: true}); err == nil {
		second.Close()
		t.Fatal("live socket was removed as stale")
	}

	regular := filepath.Join(safeParent, "regular.sock")
	if err := os.WriteFile(regular, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if created, err := Listen(Endpoint{Transport: TransportUnix, Address: regular}, ListenOptions{RecoverStaleNative: true}); err == nil {
		created.Close()
		t.Fatal("regular file was replaced by a socket")
	}
}

func echoOne(listener net.Listener, done chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		done <- err
		return
	}
	defer connection.Close()
	var request Request
	if err := json.NewDecoder(connection).Decode(&request); err != nil {
		done <- err
		return
	}
	done <- json.NewEncoder(connection).Encode(Response{
		JSONRPC: Version, ID: request.ID, Result: map[string]bool{"ok": true},
	})
}
