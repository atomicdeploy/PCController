package portowner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeEnumerator struct {
	owner Owner
	found bool
	err   error
	port  string
}

func (fake *fakeEnumerator) FindOwner(_ context.Context, port string) (Owner, bool, error) {
	fake.port = port
	return fake.owner, fake.found, fake.err
}

func TestEnrichOpenErrorUsesInjectedOwnerEnumerator(t *testing.T) {
	fake := &fakeEnumerator{found: true, owner: Owner{
		PID: 42, Name: "terminal.exe", Executable: `C:\Apps\terminal.exe`,
		Window: Window{Title: "Serial console", Class: "TerminalWindow", Visible: true},
	}}
	err := EnrichOpenErrorWith(context.Background(), "COM7", errors.New("Access is denied."), fake)
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected BusyError, got %T: %v", err, err)
	}
	if fake.port != "COM7" || busy.Owner == nil || busy.Owner.PID != 42 {
		t.Fatalf("unexpected owner resolution: port=%q busy=%+v", fake.port, busy)
	}
	for _, expected := range []string{"terminal.exe", "PID 42", `C:\Apps\terminal.exe`, "Serial console"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("diagnostic %q missing %q", err, expected)
		}
	}
}

func TestEnrichOpenErrorDegradesWhenOwnerCannotBeResolved(t *testing.T) {
	fake := &fakeEnumerator{err: errors.New("native handle query unavailable")}
	err := EnrichOpenErrorWith(context.Background(), "COM9", errors.New("Serial port busy"), fake)
	if !strings.Contains(err.Error(), "owner could not be resolved") || !strings.Contains(err.Error(), "native handle query unavailable") {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	if !errors.Is(err, err.(*BusyError).Cause) {
		t.Fatal("BusyError did not retain the original open failure")
	}
}

func TestNonBusyAndNetworkErrorsSkipEnumeration(t *testing.T) {
	for _, port := range []string{"tcp://127.0.0.1:9000", "ttyUSB0"} {
		fake := &fakeEnumerator{}
		err := EnrichOpenErrorWith(context.Background(), port, errors.New("connection refused"), fake)
		if fake.port != "" || strings.Contains(err.Error(), "serial owner") {
			t.Fatalf("unexpected owner lookup for %s: %v", port, err)
		}
	}
}

func TestTerminationRequiresExactConfirmationAndProtectsController(t *testing.T) {
	owner := Owner{PID: 77, Name: "other.exe", Executable: `C:\Apps\other.exe`}
	if err := validateTermination(owner, "yes", 12, `C:\Apps\controller.exe`); err == nil {
		t.Fatal("terminate accepted an ambiguous confirmation")
	}
	if err := validateTermination(owner, terminationConfirmation(owner), 12, `C:\Apps\controller.exe`); err != nil {
		t.Fatalf("valid external owner rejected: %v", err)
	}
	if err := validateTermination(Owner{PID: 12, Name: "other.exe"}, "TERMINATE 12", 12, `C:\Apps\controller.exe`); err == nil {
		t.Fatal("current process was not protected")
	}
	protected := Owner{PID: 99, Name: "controller.exe", Executable: `C:\Apps\controller.exe`}
	if err := validateTermination(protected, "TERMINATE 99", 12, `C:\Apps\controller.exe`); err == nil {
		t.Fatal("primary/controller executable was not protected")
	}
}

func TestOwnerScanCoordinatorSingleflightsAndCaches(t *testing.T) {
	coordinator := newOwnerScanCoordinator()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	scan := func(context.Context, string) (Owner, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return Owner{PID: 41, Name: "terminal.exe"}, true, nil
	}

	const waiters = 12
	results := make(chan ownerLookupResult, waiters)
	var wait sync.WaitGroup
	for index := 0; index < waiters; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			owner, found, err := coordinator.find(context.Background(), "com7", scan)
			results <- ownerLookupResult{owner: owner, found: found, err: err}
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("owner scan did not start")
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent native scans=%d", calls.Load())
	}
	close(release)
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil || !result.found || result.owner.PID != 41 {
			t.Fatalf("result=%#v", result)
		}
	}
	owner, found, err := coordinator.find(context.Background(), `\\.\COM7`, scan)
	if err != nil || !found || owner.PID != 41 || calls.Load() != 1 {
		t.Fatalf("cached owner=%#v found=%t err=%v calls=%d", owner, found, err, calls.Load())
	}
}

func TestOwnerScanCoordinatorDoesNotSpawnBehindStalledScan(t *testing.T) {
	coordinator := newOwnerScanCoordinator()
	release := make(chan struct{})
	var calls atomic.Int32
	scan := func(context.Context, string) (Owner, bool, error) {
		calls.Add(1)
		<-release
		return Owner{}, false, nil
	}
	firstContext, firstCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer firstCancel()
	if _, _, err := coordinator.find(firstContext, "COM8", scan); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first timeout=%v", err)
	}
	secondContext, secondCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer secondCancel()
	if _, _, err := coordinator.find(secondContext, "COM9", scan); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second timeout=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("stalled native scans=%d; expected one", calls.Load())
	}
	close(release)
}
