package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/tui"
)

func TestRemoteLiveNotificationsCoalesceAndPreserveIntentionalOff(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	status := native.Status{SupplyMV: 12_345}
	statusParams, _ := json.Marshal(controllerapi.StatusUpdate{Time: time.Unix(1, 0), Status: status})
	statusMessage, _ := json.Marshal(remoteLiveMessage{Method: "controller.status", Params: statusParams})
	if _, err := client.consumeLiveMessage(statusMessage, 0); err != nil {
		t.Fatal(err)
	}
	offEvent, _ := json.Marshal(controllerapi.Event{
		ID: 7, Time: time.Unix(2, 0), Kind: "status_led.changed",
		Opcode:  native.OpStatusLEDChanged,
		Payload: []byte{0, 0, 0, 0, 0, 255},
	})
	offMessage, _ := json.Marshal(remoteLiveMessage{Method: "controller.state", Params: offEvent})
	if _, err := client.consumeLiveMessage(offMessage, 0); err != nil {
		t.Fatal(err)
	}
	update := <-client.liveRaw
	if !update.HaveStatus || update.Status.SupplyMV != 12_345 || !update.HaveStatusLED ||
		update.StatusLED != (native.StatusLEDState{Condition: 255}) {
		t.Fatalf("coalesced update=%#v payload=%s", update, base64.StdEncoding.EncodeToString([]byte{0, 0, 0, 0, 0, 255}))
	}
}

func TestRemoteLiveCoalescerPreservesSourceOrderAndMissingEpochMarker(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	peak := native.StatusLEDState{Blue: 145, Brightness: 145, Effect: native.StatusEffectBreathe}
	baseline := tui.RemoteLiveUpdate{
		StatusLED: peak, HaveStatusLED: true, StatusLEDOrderKnown: true,
		StatusLEDEpoch: 2, StatusLEDRevision: 10,
	}
	client.publishLive(baseline)
	delayed := baseline
	delayed.StatusLED.Blue = 18
	delayed.StatusLEDRevision = 9
	client.publishLive(delayed)
	got := <-client.liveRaw
	if got.StatusLED != peak || got.StatusLEDEpoch != 2 || got.StatusLEDRevision != 10 {
		t.Fatalf("delayed frame replaced newer queued baseline: %#v", got)
	}

	client.publishLive(baseline)
	newer := baseline
	newer.StatusLED.Blue = 100
	newer.StatusLEDRevision = 11
	client.publishLive(newer)
	got = <-client.liveRaw
	if got.StatusLED != newer.StatusLED || got.StatusLEDRevision != 11 {
		t.Fatalf("newer frame did not replace queued baseline: %#v", got)
	}

	oldPrimary := tui.RemoteLiveUpdate{
		StatusLED: peak, HaveStatusLED: true, StatusLEDOrderKnown: true,
		StatusLEDEpoch: 1, StatusLEDRevision: 100,
	}
	client.publishLive(oldPrimary)
	client.publishLive(tui.RemoteLiveUpdate{
		StatusLEDOrderKnown: true, StatusLEDEpoch: 2, StatusLEDRevision: 0,
	})
	delayedOldPrimary := oldPrimary
	delayedOldPrimary.StatusLED.Blue = 18
	delayedOldPrimary.StatusLEDRevision = 101
	client.publishLive(delayedOldPrimary)
	got = <-client.liveRaw
	if !got.HaveStatusLED || got.StatusLED != peak || !got.StatusLEDOrderKnown ||
		got.StatusLEDEpoch != 2 || got.StatusLEDRevision != 0 {
		t.Fatalf("missing new-primary marker lost visual/order authority: %#v", got)
	}
}

func TestRemoteLiveCoalescerKeepsLatestLegacyFrameWithoutRevision(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	first := tui.RemoteLiveUpdate{
		StatusLED: native.StatusLEDState{Blue: 18}, HaveStatusLED: true,
		StatusLEDOrderKnown: true, StatusLEDEpoch: 1,
	}
	second := first
	second.StatusLED.Blue = 36
	client.publishLive(first)
	client.publishLive(second)
	got := <-client.liveRaw
	if got.StatusLED != second.StatusLED || got.StatusLEDEpoch != 1 || got.StatusLEDRevision != 0 {
		t.Fatalf("legacy revisionless stream did not coalesce latest value: %#v", got)
	}
}

func TestRemoteLiveSubscriptionRejectsNegativeAcknowledgement(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	if _, err := client.consumeLiveMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32003,"message":"capability denied"}}`), 1); err == nil {
		t.Fatal("negative subscription acknowledgement was ignored")
	}
	if _, err := client.consumeLiveMessage([]byte(`{"jsonrpc":"2.0","id":2,"result":{"subscribed":false}}`), 2); err == nil {
		t.Fatal("false subscription acknowledgement was ignored")
	}
}

func TestRemoteLiveSubscriptionCorrelatesLatestAcknowledgement(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	late := []byte(`{"jsonrpc":"2.0","id":1,"result":{"subscribed":true}}`)
	if acknowledged, err := client.consumeLiveMessage(late, 2); err != nil || acknowledged {
		t.Fatalf("late acknowledgement accepted=%t err=%v", acknowledged, err)
	}
	current := []byte(`{"jsonrpc":"2.0","id":2,"result":{"subscribed":true}}`)
	if acknowledged, err := client.consumeLiveMessage(current, 2); err != nil || !acknowledged {
		t.Fatalf("current acknowledgement accepted=%t err=%v", acknowledged, err)
	}
}

func TestRemoteStableStatusEmitsBoundedFreshnessHeartbeat(t *testing.T) {
	status := native.Status{SupplyMV: 12_345}
	client := &remoteTUIIPC{
		liveRaw: make(chan tui.RemoteLiveUpdate, 1), lastStatus: status,
		haveLastStatus: true, lastStatusForwardedAt: time.Now().Add(-time.Second),
	}
	params, _ := json.Marshal(controllerapi.StatusUpdate{Time: time.Now(), Status: status})
	message, _ := json.Marshal(remoteLiveMessage{Method: "controller.status", Params: params})
	if _, err := client.consumeLiveMessage(message, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-client.liveRaw:
		if !update.HaveStatus || update.Status != status {
			t.Fatalf("heartbeat=%#v", update)
		}
	default:
		t.Fatal("stable healthy status did not emit its bounded freshness heartbeat")
	}
}

func TestRemoteLiveFlushIsLatestOnlyAndPaced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &remoteTUIIPC{
		live: make(chan tui.RemoteLiveUpdate, 1), liveRaw: make(chan tui.RemoteLiveUpdate, 1),
		flushRates: make(chan time.Duration, 1), flushDone: make(chan struct{}),
		liveRate: 50 * time.Millisecond,
	}
	go client.flushLive(ctx)
	for value := int32(1); value <= 20; value++ {
		client.publishLive(tui.RemoteLiveUpdate{
			Status: native.Status{SupplyMV: value}, HaveStatus: true,
		})
	}
	select {
	case update := <-client.live:
		t.Fatalf("unpaced live render: %#v", update)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case update := <-client.live:
		if update.Status.SupplyMV != 20 {
			t.Fatalf("flush delivered %d, want latest 20", update.Status.SupplyMV)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("paced live update was not flushed")
	}
	cancel()
	<-client.flushDone
}

func TestRemoteStateStreamRejectsOutOfOrderFramesButKeepsTrueOff(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1)}
	encode := func(id uint64, state native.StatusLEDState) []byte {
		payload := []byte{state.Red, state.Green, state.Blue, state.Brightness, state.Effect, state.Condition}
		event, _ := json.Marshal(controllerapi.Event{
			ID: id, Time: time.Unix(int64(id), 0), Kind: "status_led.changed",
			Opcode: native.OpStatusLEDChanged, Payload: payload,
			Metadata: map[string]string{"revision": fmt.Sprint(id)},
		})
		message, _ := json.Marshal(remoteLiveMessage{Method: "controller.state", Params: event})
		return message
	}
	blue := native.StatusLEDState{Blue: 120, Brightness: 120, Condition: 255}
	if _, err := client.consumeLiveMessage(encode(10, blue), 0); err != nil {
		t.Fatal(err)
	}
	off := native.StatusLEDState{Condition: 255}
	if _, err := client.consumeLiveMessage(encode(9, off), 0); err != nil {
		t.Fatal(err)
	}
	if got := (<-client.liveRaw).StatusLED; got != blue {
		t.Fatalf("out-of-order frame replaced blue with %#v", got)
	}
	if _, err := client.consumeLiveMessage(encode(11, off), 0); err != nil {
		t.Fatal(err)
	}
	if got := (<-client.liveRaw).StatusLED; got != off {
		t.Fatalf("authoritative ordered off frame was hidden: %#v", got)
	}
}

func TestRemoteStateCursorPersistsAcrossSessionsAndIsSentOnSubscribe(t *testing.T) {
	client := &remoteTUIIPC{liveRaw: make(chan tui.RemoteLiveUpdate, 1), liveStateCursor: 42}
	requestID, afterID := client.nextLiveRequest()
	if requestID != 1 || afterID != 42 {
		t.Fatalf("request=%d after=%d", requestID, afterID)
	}
	if client.advanceLiveStateCursor(41) {
		t.Fatal("reconnected session accepted an older retained state frame")
	}
	if !client.advanceLiveStateCursor(43) || client.liveStateCursor != 43 {
		t.Fatalf("new state cursor=%d", client.liveStateCursor)
	}
}

func TestRemoteStateCursorResetsOnPrimaryEpochChange(t *testing.T) {
	client := &remoteTUIIPC{
		liveStateCursor: 2, liveRequestedAfter: 2,
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	if reset := client.acceptLiveAcknowledgement(10, "primary-b"); !reset {
		t.Fatal("primary event epoch reset was not detected")
	}
	if client.liveStateCursor != 10 || client.liveRequestedAfter != 10 ||
		client.liveEpoch != 2 || client.liveSessionEpoch != 2 {
		t.Fatalf("reset cursor=%d requested=%d", client.liveStateCursor, client.liveRequestedAfter)
	}
	if !client.advanceLiveStateCursor(11) {
		t.Fatal("new primary epoch state was rejected by the old cursor")
	}
	if reset := client.acceptLiveAcknowledgement(12, "primary-b"); reset {
		t.Fatal("monotonic same-epoch acknowledgement triggered a reset")
	}
}

func TestRemoteEpochResetPublishesAuthoritativeBaselineBeforeNewFrames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authoritative := RichPreviewSnapshotForRemoteTest()
	authoritative.Status.SupplyMV = 13_002
	authoritative.StatusLED = native.StatusLEDState{Condition: 255}
	authoritative.HaveStatusLED = true
	client := &remoteTUIIPC{
		ctx: ctx, liveRaw: make(chan tui.RemoteLiveUpdate, 1),
		liveStateCursor: 500, liveRequestedAfter: 500,
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		if method != "controller.snapshot" {
			return errors.New("unexpected method " + method)
		}
		wire := target.(*remoteSnapshotWire)
		wire.Connected = authoritative.Connected
		wire.Status = authoritative.Status
		wire.HaveStatus = authoritative.HaveStatus
		wire.StatusUpdated = authoritative.StatusUpdated
		wire.StatusLED = authoritative.StatusLED
		wire.HaveStatusLED = authoritative.HaveStatusLED
		wire.StatusLEDUpdated = authoritative.StatusLEDUpdated
		wire.StatusLEDRevision = 1
		wire.HostInstanceID = "primary-b"
		return nil
	}
	reset, err := client.consumeLiveEpochReset(2, "primary-b")
	if err != nil || !reset {
		t.Fatalf("reset=%t err=%v", reset, err)
	}
	baseline := <-client.liveRaw
	if !baseline.HaveStatus || baseline.Status.SupplyMV != 13_002 ||
		!baseline.HaveStatusLED || baseline.StatusLED != authoritative.StatusLED ||
		baseline.StatusLEDEpoch != 2 || baseline.StatusLEDRevision != 1 {
		t.Fatalf("authoritative restart baseline=%#v", baseline)
	}
	_, afterID := client.nextLiveRequest()
	if afterID != 2 || !client.advanceLiveStateCursor(3) {
		t.Fatalf("next after_id=%d cursor=%d", afterID, client.liveStateCursor)
	}
}

func TestRemoteSnapshotDiscardsOldPrimaryResponseAfterLiveIdentitySwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &remoteTUIIPC{
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		if method != "controller.snapshot" {
			return errors.New("unexpected method " + method)
		}
		wire := target.(*remoteSnapshotWire)
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			wire.HostInstanceID = "primary-a"
			wire.StatusLEDRevision = 99
			return nil
		}
		wire.HostInstanceID = "primary-b"
		wire.StatusLEDRevision = 2
		return nil
	}
	result := make(chan control.Snapshot, 1)
	go func() {
		snapshot, _ := client.Snapshot(ctx)
		result <- snapshot
	}()
	<-firstStarted
	if !client.acceptLiveAcknowledgement(10, "primary-b") {
		t.Fatal("live identity switch was not detected")
	}
	close(releaseFirst)
	snapshot := <-result
	if calls.Load() != 2 || snapshot.StatusLEDEpoch != 2 || snapshot.StatusLEDRevision != 2 ||
		client.liveInstanceID != "primary-b" {
		t.Fatalf("stale A response was relabeled/adopted: calls=%d snapshot=%#v client=%#v",
			calls.Load(), snapshot, client)
	}
}

func TestRemotePostBaselinePreAcknowledgementFrameUsesNewEpoch(t *testing.T) {
	client := &remoteTUIIPC{
		liveRaw:         make(chan tui.RemoteLiveUpdate, 1),
		liveStateCursor: 2, liveRequestedAfter: 2,
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	if !client.acceptLiveAcknowledgement(10, "primary-b") {
		t.Fatal("restart was not detected")
	}
	off := native.StatusLEDState{Condition: 255}
	payload := []byte{off.Red, off.Green, off.Blue, off.Brightness, off.Effect, off.Condition}
	event, _ := json.Marshal(controllerapi.Event{
		ID: 11, Time: time.Unix(11, 0), Kind: "status_led.changed",
		Opcode: native.OpStatusLEDChanged, Payload: payload,
		Metadata: map[string]string{"revision": "2"},
	})
	message, _ := json.Marshal(remoteLiveMessage{Method: "controller.state", Params: event})
	if _, err := client.consumeLiveMessage(message, 0); err != nil {
		t.Fatal(err)
	}
	update := <-client.liveRaw
	if update.StatusLEDEpoch != 2 || update.StatusLEDRevision != 2 || update.StatusLED != off {
		t.Fatalf("post-baseline frame used stale session epoch: %#v", update)
	}
}

func TestRemoteStateBeforeAcknowledgementWaitsForSourceIdentity(t *testing.T) {
	client := &remoteTUIIPC{
		liveRaw:         make(chan tui.RemoteLiveUpdate, 1),
		liveStateCursor: 10, liveRequestedAfter: 10,
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	event, _ := json.Marshal(controllerapi.Event{
		ID: 11, Time: time.Unix(11, 0), Kind: "status_led.changed",
		Opcode: native.OpStatusLEDChanged, Payload: []byte{0, 0, 8, 145, 1, 9},
		Metadata: map[string]string{"revision": "1"},
	})
	message, _ := json.Marshal(remoteLiveMessage{Method: "controller.state", Params: event})
	var pending [][]byte
	acknowledged, err := client.consumeOrBufferLiveMessage(message, 1, false, &pending)
	if err != nil || acknowledged || len(pending) != 1 || client.liveStateCursor != 10 {
		t.Fatalf("pre-ack frame was not quarantined: ack=%t err=%v pending=%d cursor=%d",
			acknowledged, err, len(pending), client.liveStateCursor)
	}
	select {
	case update := <-client.liveRaw:
		t.Fatalf("pre-ack frame leaked under the previous identity: %#v", update)
	default:
	}
	if !client.acceptLiveAcknowledgement(10, "primary-b") {
		t.Fatal("primary restart was not adopted from the acknowledgement")
	}
	if _, err := client.consumeLiveMessage(pending[0], 0); err != nil {
		t.Fatal(err)
	}
	update := <-client.liveRaw
	if update.StatusLEDEpoch != 2 || update.StatusLEDRevision != 1 || update.StatusLED.Blue != 8 {
		t.Fatalf("buffered frame was not replayed under primary B: %#v", update)
	}
}

func TestRemoteEpochResetPublishesMissingLEDOrderWatermark(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &remoteTUIIPC{
		ctx: ctx, liveRaw: make(chan tui.RemoteLiveUpdate, 1),
		liveStateCursor: 100, liveRequestedAfter: 100,
		liveEpoch: 1, liveInstanceID: "primary-a",
		liveSessionEpoch: 1, liveSessionInstanceID: "primary-a",
	}
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		if method != "controller.snapshot" {
			return errors.New("unexpected method " + method)
		}
		wire := target.(*remoteSnapshotWire)
		wire.HostInstanceID = "primary-b"
		wire.HaveStatusLED = false
		wire.StatusLEDRevision = 0
		return nil
	}
	reset, err := client.consumeLiveEpochReset(2, "primary-b")
	if err != nil || !reset {
		t.Fatalf("reset=%t err=%v", reset, err)
	}
	baseline := <-client.liveRaw
	if baseline.HaveStatusLED || !baseline.StatusLEDOrderKnown ||
		baseline.StatusLEDEpoch != 2 || baseline.StatusLEDRevision != 0 {
		t.Fatalf("missing new-primary baseline lost its order watermark: %#v", baseline)
	}
}

func RichPreviewSnapshotForRemoteTest() control.Snapshot {
	return control.Snapshot{
		Connected: true, HaveStatus: true, StatusUpdated: time.Unix(10, 0),
		StatusLEDUpdated: time.Unix(10, 0),
	}
}

func TestRemoteTUIPollEventsBacksOffAndRediscoversAfterTransportFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &remoteTUIIPC{
		events: make(chan control.Event, 1), ready: make(chan struct{}),
		done: make(chan struct{}), retry: 80 * time.Millisecond,
	}
	var calls atomic.Int32
	fourthCall := make(chan struct{})
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		call := calls.Add(1)
		if call == 1 {
			if method != "controller.event.latest" {
				t.Fatalf("first method=%q", method)
			}
			target.(*struct {
				ID uint64 `json:"id"`
			}).ID = 9
			return nil
		}
		if call == 2 && method != "controller.event.next" {
			t.Errorf("second method=%q, want event.next", method)
		}
		if (call == 3 || call == 4) && method != "controller.event.latest" {
			t.Errorf("call %d method=%q, want event.latest", call, method)
		}
		if call == 4 {
			close(fourthCall)
		}
		return errors.New("primary unavailable")
	}
	started := time.Now()
	go client.pollEvents(ctx)
	select {
	case <-fourthCall:
	case <-time.After(time.Second):
		t.Fatal("poller did not retry")
	}
	cancel()
	<-client.done
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("transport failure retried without backoff: %s", elapsed)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("RPC calls=%d, want one backoff then cursor rediscovery", got)
	}
}

func TestRemoteTUIInstanceLeaseReportsRefreshesAndRemoves(t *testing.T) {
	reporter, err := hostui.NewNavigationReporter(true, "")
	if err != nil {
		t.Fatal(err)
	}
	client := &remoteTUIIPC{sessions: make(chan struct{}, 1)}
	var mu sync.Mutex
	methods := make([]string, 0, 4)
	reports := make([]hostui.AppInstance, 0, 4)
	refreshed := make(chan struct{}, 1)
	catchUp := make(chan struct{}, 1)
	client.callFn = func(_ context.Context, method string, params any, _ any) error {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, method)
		if method == "controller.app.instance.report" {
			report := params.(hostui.AppInstance)
			reports = append(reports, report)
			if len(reports) >= 2 {
				select {
				case refreshed <- struct{}{}:
				default:
				}
			}
			if report.Values[hostui.NavigationCatchUpKey] == "true" {
				select {
				case catchUp <- struct{}{}:
				default:
				}
			}
		}
		return nil
	}
	lease := newRemoteTUIInstanceLease(client, reporter, 15*time.Millisecond)
	if err := lease.Update("events", "PCController — Activity"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("instance lease did not refresh")
	}
	client.sessions <- struct{}{}
	select {
	case <-catchUp:
	case <-time.After(time.Second):
		t.Fatal("instance lease did not request canonical catch-up after reconnect")
	}
	lease.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(reports) < 2 {
		t.Fatalf("instance reports=%d methods=%#v", len(reports), methods)
	}
	first, last := reports[0], reports[len(reports)-1]
	if first.ID != reporter.InstanceID() || first.Surface != "tui" || first.Page != "events" ||
		first.LeaseSeconds != remoteTUIInstanceLeaseSeconds || first.Self == nil ||
		first.Values[hostui.NavigationSyncKey] != hostui.NavigationSyncFollow ||
		first.Values[hostui.NavigationGroupKey] != hostui.DefaultNavigationGroup ||
		first.Values["terminal_title"] != "PCController — Activity" {
		t.Fatalf("first report=%#v", first)
	}
	if first.Values[hostui.NavigationRevisionKey] >= last.Values[hostui.NavigationRevisionKey] {
		t.Fatalf("non-monotonic reports first=%#v last=%#v", first.Values, last.Values)
	}
	if methods[len(methods)-1] != "controller.app.instance.remove" {
		t.Fatalf("last method=%q methods=%#v", methods[len(methods)-1], methods)
	}
}

func TestRemoteTUIPollerEmitsAuthoritativeSessionResetBeforeNewEpoch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &remoteTUIIPC{
		events: make(chan control.Event, 4), ready: make(chan struct{}),
		done: make(chan struct{}), retry: time.Millisecond,
	}
	var nextCalls atomic.Int32
	client.callFn = func(_ context.Context, method string, _ any, target any) error {
		switch method {
		case "controller.event.latest":
			latest := target.(*struct {
				ID uint64 `json:"id"`
			})
			if nextCalls.Load() == 0 {
				latest.ID = 20
			} else {
				latest.ID = 1
			}
			return nil
		case "controller.event.next":
			if nextCalls.Add(1) == 1 {
				return errors.New("primary restarted")
			}
			<-ctx.Done()
			return ctx.Err()
		default:
			return errors.New("unexpected method")
		}
	}
	go client.pollEvents(ctx)
	select {
	case event := <-client.events:
		if event.Kind != "client.navigation.session.reset" {
			t.Fatalf("reset event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("poller did not emit session reset")
	}
	cancel()
	<-client.done
}

func TestRemoteTUICommandEngineMirrorsPrimaryCatalog(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	primaryEngine := shell.New(10)
	if err := primaryEngine.Register(shell.Command{
		Name: "echo", Aliases: []string{"say"}, Usage: "echo VALUE", Summary: "test command",
		Run: func(_ context.Context, args []string) (string, error) {
			return shell.Join(args), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	serverContext, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	server, err := startPrimaryIPCAt(serverContext, "127.0.0.1:0", runtime, primaryEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client := newRemoteTUIIPC(context.Background(), server.listener.Addr().String(), "")
	defer client.Close()
	requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	remoteEngine, err := client.CommandEngine(requestContext)
	if err != nil {
		t.Fatal(err)
	}
	output, err := remoteEngine.Execute(requestContext, `say "hello remote board"`)
	if err != nil {
		t.Fatal(err)
	}
	if output != `"hello remote board"` {
		t.Fatalf("remote command output=%q", output)
	}
	if snapshot, err := client.Snapshot(requestContext); err != nil || snapshot.Connected {
		t.Fatalf("remote snapshot=%#v err=%v", snapshot, err)
	}
}

func TestRemoteControlEventPreservesTUIFields(t *testing.T) {
	value := remoteControlEvent(controllerapi.Event{
		ID: 7, Kind: "status", Stream: "activity", Text: "updated",
		Opcode: 0x81, Seq: 3, Payload: []byte{1, 2}, Source: "board",
		Metadata: map[string]string{"page": "events"}, RFCode: 0x1234,
	})
	if value.ID != 7 || value.Frame.Opcode != 0x81 || value.Frame.Seq != 3 ||
		value.Source != "board" || value.Metadata["page"] != "events" || value.RFCode != 0x1234 {
		t.Fatalf("converted event=%#v", value)
	}
}
