package ipcjson

import (
	"strings"
	"testing"
)

func TestSOAPActionCatalogIncludesBoardOperationsEventsAndOpcodes(t *testing.T) {
	for _, action := range []string{"GetStatus", "GetBoardIdentity", "GetProtocolInfo", "GetCommandCatalog", "GetEventInfo", "GetOpcodeInfo", "GetPublicInfo"} {
		if got := soapActionFromBody([]byte("<u:" + action + "/>")); got != action {
			t.Fatalf("soap action %q resolved as %q", action, got)
		}
	}
	if strings.Contains(soapActionFromBody([]byte("<u:Unknown/>")), "Unknown") {
		t.Fatal("unknown SOAP action was accepted")
	}
}
