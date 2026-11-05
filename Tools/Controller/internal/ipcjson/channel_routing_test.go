package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func routingTestService(t *testing.T) (*Service, *appconfig.Config) {
	service, config := browserUIConfigTestService(t)
	service.HostInstanceID = "test-host"
	config.Connection.LastDevice = &appconfig.DeviceIdentity{SerialNumber: "routing-test"}
	config.BoardProfiles = map[string]appconfig.BoardProfile{"serial:routing-test": {Key: "cinema", Mode: appconfig.BoardModeCinemaSeatMotion, ChannelRouting: appconfig.ChannelRouting{Enabled: true, Channels: []appconfig.SemanticChannel{
		{Key: "lighting.aisle", Kind: "relay", Target: "relay.5", Name: "Aisle", Icon: "lamp"},
		{Key: "lighting.screen", Kind: "relay", Target: "relay.6", Name: "Screen"},
	}}}}
	return service, config
}

func TestChannelRoutingSaveDoesNotReenterConfigurationProvider(t *testing.T) {
	service, config := routingTestService(t)
	runtime := control.New(control.Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	service.Client = controller.AttachSharedRuntime(runtime, shell.New(8))
	inSave := false
	control.NewCommandEngine(runtime, control.CommandOptions{HostConfig: func() appconfig.Config {
		if inSave {
			t.Error("routing save re-entered configuration provider")
		}
		return *config
	}})
	save := service.UpdateHostConfig
	service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		inSave = true
		defer func() { inSave = false }()
		return save(change)
	}
	before := service.channelRoutingCatalog()
	next := before.Routing
	next.Enabled = false
	if _, err := service.updateChannelRouting(context.Background(), channelRoutingUpdate{before.BoardProfile.BoardIdentity, before.BoardProfile.Revision, &next}); err != nil {
		t.Fatal(err)
	}
}

func TestChannelRoutingRPCAtomicSwapAndConflict(t *testing.T) {
	service, _ := routingTestService(t)
	afterEvent := service.Client.LatestEventID()
	before := service.channelRoutingCatalog()
	next := before.Routing
	next.Channels = append([]appconfig.SemanticChannel{}, next.Channels...)
	next.Channels[0].Target, next.Channels[1].Target = next.Channels[1].Target, next.Channels[0].Target
	params, _ := json.Marshal(channelRoutingUpdate{before.BoardProfile.BoardIdentity, before.BoardProfile.Revision, &next})
	response := service.Dispatch(context.Background(), Request{Method: "controller.channel_routing.update", Params: params})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	after := response.Result.(channelRoutingCatalog)
	if after.Routing.Channels[0].Target != "relay.6" || after.Routing.Channels[0].Name != "Aisle" || after.BoardProfile.Revision == before.BoardProfile.Revision {
		t.Fatalf("swap=%+v", after)
	}
	if len(after.Targets) != 15 {
		t.Fatalf("targets=%d", len(after.Targets))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := service.Client.NextEvent(ctx, afterEvent, "peripherals.changed")
	if err != nil || event.Metadata["board_identity"] != after.BoardProfile.BoardIdentity || event.Metadata["revision"] != after.BoardProfile.Revision || event.Metadata["changed_fields"] != "channel_routing" {
		t.Fatalf("missing authoritative catalog event: %+v %v", event, err)
	}
	if response := service.Dispatch(context.Background(), Request{Method: "controller.channel_routing.update", Params: params}); response.Error == nil {
		t.Fatal("stale revision accepted")
	}
	if service.peripheralSettings().ChannelRouting.Channels[0].Key != "lighting.aisle" {
		t.Fatal("shared peripheral catalog omitted semantic channel")
	}
	for _, raw := range []string{`{}`, `{"routing":{"enabled":false,"channels":[]}}`, `{"board_identity":"serial:routing-test","expected_revision":"stale","routing":null}`, `{"unexpected":true}`} {
		if response := service.Dispatch(context.Background(), Request{Method: "controller.channel_routing.update", Params: json.RawMessage(raw)}); response.Error == nil {
			t.Fatalf("invalid mutation %s accepted", raw)
		}
	}
	if requestCapability("controller.channel_routing.update", nil) != capabilityHostConfig || requestCapability("controller.channel_routing.get", nil) != capabilityRead || requestCapability("controller.channel.set", nil) != capabilityBoard {
		t.Fatal("incorrect authorization classification")
	}
}

func TestChannelRoutingSaveDoesNotRelabelReplacedOwner(t *testing.T) {
	service, config := routingTestService(t)
	before := service.channelRoutingCatalog()
	save := service.UpdateHostConfig
	service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		if err := save(change); err != nil {
			return err
		}
		config.Connection.LastDevice = &appconfig.DeviceIdentity{SerialNumber: "replacement"}
		return nil
	}
	next := before.Routing
	next.Enabled = false
	result, err := service.updateChannelRouting(context.Background(), channelRoutingUpdate{before.BoardProfile.BoardIdentity, before.BoardProfile.Revision, &next})
	if err != nil || result.BoardProfile.BoardIdentity != before.BoardProfile.BoardIdentity || result.Routing.Enabled {
		t.Fatalf("save attributed to wrong owner: %+v %v", result, err)
	}
}

func TestChannelRoutingPhysicalOutputRPCValidation(t *testing.T) {
	service, _ := routingTestService(t)
	for _, raw := range []string{`{}`, `{"target":"relay.5","value":1}`, `{"unknown":true}`} {
		reply := service.Dispatch(context.Background(), Request{Method: "controller.output.set", Params: json.RawMessage(raw)})
		if reply.Error == nil {
			t.Fatal("unfenced output accepted")
		}
		data, ok := reply.Error.Data.(map[string]any)
		if !ok || data["outcome"] != "rejected" {
			t.Fatalf("untyped rejection: %+v", reply.Error)
		}
	}
	uncertain := &RPCError{Code: -32000, Message: "transport lost", Data: map[string]any{"outcome": "unknown"}}
	if outputRPCError(uncertain) != uncertain {
		t.Fatal("uncertain outcome reclassified")
	}
	if requestCapability("controller.output.set", nil) != capabilityBoard {
		t.Fatal("physical write authorization")
	}
}

func TestChannelRoutingLogicalPWMUsesAuthoritativeCurve(t *testing.T) {
	ui := appconfig.Defaults().UI
	percent, raw := uint32(50000), uint16(1234)
	request := outputValueRequest{Target: "pwm.0", PercentMilli: &percent, ExpectedPWMConfigurationRevision: appconfig.PWMConfigurationRevision(ui)}
	want := appconfig.PWMRawFromPercent(50, appconfig.PWMChannel(ui, 0))
	if got, err := resolvePinnedOutputValue(request, ui); err != nil || got != want || got == 2048 {
		t.Fatalf("curve bypassed: %d %v", got, err)
	}
	config := appconfig.PWMChannel(ui, 0)
	config.Curve, config.Gamma = "linear", 1
	ui.PWMChannels["pwm.0"] = config
	if _, err := resolvePinnedOutputValue(request, ui); err == nil {
		t.Fatal("stale PWM configuration accepted")
	}
	request.ExpectedPWMConfigurationRevision = appconfig.PWMConfigurationRevision(ui)
	if got, err := resolvePinnedOutputValue(request, ui); err != nil || got != 2048 {
		t.Fatalf("linear mapping %d %v", got, err)
	}
	request.Value = &raw
	if _, err := resolvePinnedOutputValue(request, ui); err == nil {
		t.Fatal("ambiguous logical and raw value")
	}
	request.Value = nil
	percent = 100001
	if _, err := resolvePinnedOutputValue(request, ui); err == nil {
		t.Fatal("out-of-range logical value")
	}
	percent = 0
	request.Target = "relay.5"
	if _, err := resolvePinnedOutputValue(request, ui); err == nil {
		t.Fatal("logical PWM sent to relay")
	}
	request.PercentMilli, request.Value, request.Target = nil, &raw, "pwm.0"
	if got, err := resolvePinnedOutputValue(request, ui); err != nil || got != raw {
		t.Fatal("raw physical value reinterpreted")
	}
}

func TestChannelRoutingSaveFailureDoesNotMutateSnapshot(t *testing.T) {
	service, config := routingTestService(t)
	before := service.channelRoutingCatalog()
	service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		candidate := *config
		if err := change(&candidate); err != nil {
			return err
		}
		return errors.New("disk full")
	}
	next := before.Routing
	next.Channels = append([]appconfig.SemanticChannel{}, next.Channels...)
	next.Channels[0].Target = "relay.7"
	if _, err := service.updateChannelRouting(context.Background(), channelRoutingUpdate{before.BoardProfile.BoardIdentity, before.BoardProfile.Revision, &next}); err == nil {
		t.Fatal("save failure hidden")
	}
	if after := service.channelRoutingCatalog(); after.BoardProfile.Revision != before.BoardProfile.Revision || after.Routing.Channels[0].Target != "relay.5" {
		t.Fatal("failed save mutated live routing")
	}
}

func TestChannelRoutingOwnerFenceAndNoDisconnectedActuation(t *testing.T) {
	service, _ := routingTestService(t)
	current := service.channelRoutingCatalog()
	value := uint16(1)
	for _, identity := range []string{"", "serial:another", current.BoardProfile.BoardIdentity} {
		params, _ := json.Marshal(channelValueRequest{ExpectedHostInstanceID: "test-host", ExpectedConnectionGeneration: 1, BoardIdentity: identity, ExpectedRevision: current.BoardProfile.Revision, Key: "lighting.aisle", Value: &value})
		if reply := service.Dispatch(context.Background(), Request{Method: "controller.channel.set", Params: params}); reply.Error == nil {
			t.Fatal("disconnected or replaced owner admitted")
		}
	}
}

func TestChannelRoutingRequiresKnownIdleOutputs(t *testing.T) {
	routing := appconfig.ChannelRouting{Enabled: true, Channels: []appconfig.SemanticChannel{{Key: "light", Kind: "relay", Target: "relay.5", Name: "Light"}}}
	snapshot := controller.Snapshot{Connected: true, HaveStatus: true, StatusUpdated: time.Now()}
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err != nil {
		t.Fatal(err)
	}
	snapshot.Status.ActiveRelays = 1 << 4
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err == nil {
		t.Fatal("active relay remapped")
	}
	snapshot.Status.ActiveRelays = 0
	snapshot.StatusUpdated = time.Time{}
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err == nil {
		t.Fatal("stale relay remapped")
	}
	routing.Channels[0].Kind, routing.Channels[0].Target = "pwm", "pwm.0"
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err == nil {
		t.Fatal("unknown PWM remapped")
	}
	snapshot.PWMValues = &native.PWMValues{Available: true}
	snapshot.PWMUpdated = time.Now()
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err != nil {
		t.Fatal(err)
	}
	snapshot.PWMValues.Values[0] = 1
	if err := requireIdleRoutingOutputs(snapshot, routing, routing); err == nil {
		t.Fatal("active PWM remapped")
	}
}
