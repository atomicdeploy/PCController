package ipcjson

import (
	"encoding/json"
	"testing"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

func TestNativeControlIntentRequiresCompleteOwner(t *testing.T) {
	service, config := routingTestService(t)
	profile := service.channelRoutingCatalog().BoardProfile
	owner := controlIntentOwner{HostInstanceID: service.HostInstanceID, Generation: 4, BoardIdentity: profile.BoardIdentity, Revision: profile.Revision, PWMRevision: appconfig.PWMConfigurationRevision(config.UI)}
	snapshot := controller.Snapshot{Connected: true, ConnectionGeneration: 4}
	if err := validateControlIntent(owner, service.HostInstanceID, *config, snapshot, true); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"host", "generation", "identity", "revision", "pwm"} {
		stale := owner
		switch field {
		case "host":
			stale.HostInstanceID = "replacement"
		case "generation":
			stale.Generation++
		case "identity":
			stale.BoardIdentity = "another-board"
		case "revision":
			stale.Revision = "old"
		case "pwm":
			stale.PWMRevision = "old"
		}
		if err := validateControlIntent(stale, service.HostInstanceID, *config, snapshot, true); err == nil {
			t.Fatalf("accepted stale %s", field)
		}
	}
	// Offline snapshots cannot be revived by a formerly valid catalog.
	if err := validateControlIntent(owner, service.HostInstanceID, *config, controller.Snapshot{}, true); err == nil {
		t.Fatal("offline owner accepted")
	}
	for _, partial := range []controlIntentOwner{{HostInstanceID: service.HostInstanceID}, {Generation: 4}, {BoardIdentity: profile.BoardIdentity}, {PWMRevision: owner.PWMRevision}} {
		if err := validateControlIntent(partial, service.HostInstanceID, *config, controller.Snapshot{}, false); err == nil {
			t.Fatal("partial fence downgraded to legacy")
		}
	}
	if err := validateControlIntent(controlIntentOwner{}, service.HostInstanceID, *config, controller.Snapshot{}, true); err != nil {
		t.Fatal("legacy clients changed contract")
	}
	var params struct {
		controlIntentOwner
		ActionID string `json:"action_id"`
	}
	raw := []byte(`{"action_id":"relay.5.on","expected_host_instance_id":"host","expected_connection_generation":4,"board_identity":"board","expected_revision":"revision","expected_pwm_configuration_revision":"pwm"}`)
	if err := decodeStrictParams(raw, &params); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(params)
	if params.Generation != 4 || params.Revision != "revision" || !json.Valid(encoded) {
		t.Fatal("embedded intent fields did not decode")
	}
}
