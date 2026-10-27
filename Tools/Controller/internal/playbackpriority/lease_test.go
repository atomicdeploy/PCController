package playbackpriority

import (
	"errors"
	"reflect"
	"testing"
)

func TestLeaseRestoresOriginalOnlyAfterLastOwner(t *testing.T) {
	current := normalClass
	var writes []uint32
	m := manager{get: func() (uint32, error) { return current, nil }, set: func(p uint32) error { current = p; writes = append(writes, p); return nil }}
	a, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err = a(); err != nil {
		t.Fatal(err)
	}
	if err = a(); err != nil {
		t.Fatal(err)
	}
	if current != highClass {
		t.Fatal("first/duplicate release revoked the other owner")
	}
	if err = b(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(writes, []uint32{highClass, normalClass}) {
		t.Fatalf("priority transitions %v", writes)
	}
}

func TestLeasePreservesExistingAndExternalPriorities(t *testing.T) {
	for _, initial := range []uint32{highClass, realtimeClass} {
		writes := 0
		m := manager{get: func() (uint32, error) { return initial, nil }, set: func(uint32) error { writes++; return nil }}
		release, err := m.acquire()
		if err != nil {
			t.Fatal(err)
		}
		if err = release(); err != nil {
			t.Fatal(err)
		}
		if writes != 0 {
			t.Fatal("changed preexisting high/realtime policy")
		}
	}
	current := normalClass
	m := manager{get: func() (uint32, error) { return current, nil }, set: func(p uint32) error { current = p; return nil }}
	release, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	current = 0x4000 // External owner selects BelowNormal during playback.
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if current != 0x4000 {
		t.Fatal("overwrote external priority change")
	}
}

func TestLeaseAcquisitionFailureDoesNotCreateOwner(t *testing.T) {
	failure := errors.New("priority unavailable")
	for _, readFailure := range []bool{true, false} {
		m := manager{get: func() (uint32, error) {
			if readFailure {
				return 0, failure
			}
			return normalClass, nil
		}, set: func(uint32) error { return failure }}
		release, err := m.acquire()
		if !errors.Is(err, failure) || release != nil || m.refs != 0 || m.changed {
			t.Fatalf("failure leaked ownership: %+v, %v", m, err)
		}
	}
}

func TestFailedRestoreRetainsOriginalBaselineForNextRelease(t *testing.T) {
	current := normalClass
	failure := errors.New("restore failed")
	fail := true
	m := manager{get: func() (uint32, error) { return current, nil }, set: func(p uint32) error {
		if p == normalClass && fail {
			return failure
		}
		current = p
		return nil
	}}
	release, err := m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err = release(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !m.changed || m.refs != 0 {
		t.Fatal("lost failed restoration ownership")
	}
	fail = false
	release, err = m.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	if current != normalClass || m.changed {
		t.Fatal("failed restore baseline was overwritten")
	}
}
