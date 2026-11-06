package diagnostics

import (
	"strings"
	"time"
)

const (
	EndpointInfo        = "/info"
	EndpointUpdateInfo  = "/update/info"
	EndpointWiFiSignal  = "/wifi/signal"
	EndpointWiFiStatus  = "/wifi/status"
	EndpointWiFiCountry = "/wifi/country"
	EndpointDescription = "/description.xml"
	EndpointConfigAP    = "/config/ap"
	EndpointConfigST    = "/config/st"
)

type Format string

const (
	FormatUnknown Format = "unknown"
	FormatJSON    Format = "json"
	FormatText    Format = "text"
	FormatXML     Format = "xml"
)

// EndpointSpec is the immutable registry entry for one audited ESP/device
// diagnostic source. Paths intentionally remain exact and query-free.
//
// Public sources may be selected through diagnostics.fetch. Non-public sources
// are polled only by the diagnostics projector. This distinction is important
// for the firmware's /config/ap and /config/st handlers: they are read-only,
// but their responses also contain a plaintext password.
type EndpointSpec struct {
	ID        string        `json:"id"`
	Path      string        `json:"path"`
	Label     string        `json:"label"`
	Hint      Format        `json:"format_hint"`
	TTL       time.Duration `json:"-"`
	Auto      bool          `json:"auto"`
	Static    bool          `json:"static"`
	Public    bool          `json:"public"`
	Sensitive bool          `json:"sensitive,omitempty"`
}

var endpointSpecs = [...]EndpointSpec{
	{
		ID:     "device-info",
		Path:   EndpointInfo,
		Label:  "Device information",
		Hint:   FormatText,
		TTL:    2 * time.Minute,
		Auto:   true,
		Public: true,
	},
	{
		ID:     "wifi-signal",
		Path:   EndpointWiFiSignal,
		Label:  "Wi-Fi signal",
		Hint:   FormatText,
		TTL:    30 * time.Second,
		Auto:   true,
		Public: true,
	},
	{
		ID:     "wifi-status",
		Path:   EndpointWiFiStatus,
		Label:  "Wi-Fi status",
		Hint:   FormatText,
		TTL:    time.Minute,
		Auto:   true,
		Public: true,
	},
	{
		ID:     "wifi-country",
		Path:   EndpointWiFiCountry,
		Label:  "Wi-Fi country",
		Hint:   FormatText,
		TTL:    12 * time.Hour,
		Auto:   true,
		Static: true,
		Public: true,
	},
	{
		ID:     "update-info",
		Path:   EndpointUpdateInfo,
		Label:  "Firmware image",
		Hint:   FormatJSON,
		TTL:    6 * time.Hour,
		Auto:   true,
		Static: true,
		Public: true,
	},
	{
		ID:     "description",
		Path:   EndpointDescription,
		Label:  "Device description",
		Hint:   FormatXML,
		TTL:    12 * time.Hour,
		Auto:   true,
		Static: true,
		Public: true,
	},
	{
		ID:        "station-config",
		Path:      EndpointConfigST,
		Label:     "Wi-Fi station configuration",
		Hint:      FormatText,
		TTL:       30 * time.Minute,
		Auto:      true,
		Sensitive: true,
	},
	{
		ID:        "access-point-config",
		Path:      EndpointConfigAP,
		Label:     "Wi-Fi access-point configuration",
		Hint:      FormatText,
		TTL:       6 * time.Hour,
		Auto:      true,
		Static:    true,
		Sensitive: true,
	},
}

func EndpointSpecs() []EndpointSpec {
	result := make([]EndpointSpec, len(endpointSpecs))
	copy(result, endpointSpecs[:])
	return result
}

func SafeEndpoints() []string {
	result := make([]string, 0, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if spec.Public {
			result = append(result, spec.Path)
		}
	}
	return result
}

// PollEndpoints returns public diagnostics plus the two internal,
// projection-only network configuration sources.
func PollEndpoints() []string {
	result := make([]string, 0, len(endpointSpecs))
	for _, spec := range endpointSpecs {
		if spec.Auto {
			result = append(result, spec.Path)
		}
	}
	return result
}

func LookupEndpoint(path string) (EndpointSpec, bool) {
	path = strings.TrimSpace(path)
	for _, spec := range endpointSpecs {
		if spec.Path == path {
			return spec, true
		}
	}
	return EndpointSpec{}, false
}

func IsEndpoint(path string) bool {
	_, ok := LookupEndpoint(path)
	return ok
}

func IsPublicEndpoint(path string) bool {
	spec, ok := LookupEndpoint(path)
	return ok && spec.Public
}

func IsSensitiveEndpoint(path string) bool {
	spec, ok := LookupEndpoint(path)
	return ok && spec.Sensitive
}
