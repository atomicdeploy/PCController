package ports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial/enumerator"
)

type Change struct {
	At     time.Time
	Reason string
}

const (
	HardwareProblemUSBDescriptorFailure = "usb_descriptor_failure"
	HardwareProblemCannotStart          = "device_cannot_start"
	HardwareProblemDisabled             = "device_disabled"
	HardwareProblemDriverMissing        = "device_driver_missing"
	HardwareProblemDriverFailure        = "device_driver_failure"
	HardwareProblemReported             = "device_reported_problem"
	HardwareProblemRemovalPending       = "device_removal_pending"
	HardwareProblemUnknown              = "device_problem"

	HardwareImpactActiveOutcomeUnknown = "active_operation_outcome_unknown"
)

// HardwareProblem is normalized operating-system evidence for a broken
// controller transport. Code is stable and suitable for UI localization;
// device strings remain diagnostic evidence and must not be parsed for UI
// behavior. Problems are returned only after correlation with the configured
// or last authenticated controller identity.
type HardwareProblem struct {
	Code          string    `json:"code"`
	Severity      string    `json:"severity"`
	Impact        string    `json:"impact,omitempty"`
	OSProblemCode uint32    `json:"os_problem_code,omitempty"`
	DeviceID      string    `json:"device_id,omitempty"`
	HardwareIDs   []string  `json:"hardware_ids,omitempty"`
	Description   string    `json:"description,omitempty"`
	Class         string    `json:"class,omitempty"`
	Location      string    `json:"location,omitempty"`
	LocationPaths []string  `json:"location_paths,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
}

type AmbiguousError struct {
	Candidates []Info
}

func (err *AmbiguousError) Error() string {
	labels := make([]string, 0, len(err.Candidates))
	for _, candidate := range err.Candidates {
		labels = append(labels, candidate.Label())
	}
	return "multiple serial devices match; select one by COM ID, serial, " +
		"instance ID, or interactive menu: " + strings.Join(labels, "; ")
}

// WatchChanges reports serial-device topology changes. Windows uses registry
// change notifications from the Plug-and-Play serial map; other platforms use
// their platform implementation.
func WatchChanges(ctx context.Context) (<-chan Change, error) {
	return watchPlatformChanges(ctx)
}

// ListHardwareProblems asks the operating system for present device failures
// and returns only failures tied to the selected or last authenticated
// controller. Platforms without trustworthy device-manager evidence return an
// empty list instead of synthesizing a fault from a serial-open error.
func ListHardwareProblems(filter Filter) ([]HardwareProblem, error) {
	return listPlatformHardwareProblems(filter)
}

type Info struct {
	Name         string
	IsUSB        bool
	VID          string
	PID          string
	SerialNumber string
	Manufacturer string
	Product      string
	FriendlyName string
	InstanceID   string
}

type Identity struct {
	Port         string
	VID          string
	PID          string
	SerialNumber string
	Name         string
	InstanceID   string
}

type Filter struct {
	Port         string
	VID          string
	PID          string
	Name         string
	SerialNumber string
	InstanceID   string
	Preferred    Identity
}

const detailedPortEnumerationSource = "go.bug.st/serial detailed enumerator; Windows SetupAPI Ports class with DIGCF_PRESENT"

// EnumerationSource identifies the live OS source authoritative for port lists.
func EnumerationSource() string { return detailedPortEnumerationSource }

func List() ([]Info, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate serial ports: %w", err)
	}
	result := detailedPortsToInfo(details)
	result = enrichPlatform(result)
	sort.SliceStable(result, func(i, j int) bool {
		return portSortKey(result[i].Name) < portSortKey(result[j].Name)
	})
	return result, nil
}

// detailedPortsToInfo is independent of CIM/WMI. On Windows, the dependency
// supplies the present SetupAPI Ports-class devnodes.
func detailedPortsToInfo(details []*enumerator.PortDetails) []Info {
	result := make([]Info, 0, len(details))
	for _, detail := range details {
		if detail == nil {
			continue
		}
		result = append(result, Info{
			Name:         detail.Name,
			IsUSB:        detail.IsUSB,
			VID:          strings.ToUpper(detail.VID),
			PID:          strings.ToUpper(detail.PID),
			SerialNumber: detail.SerialNumber,
			Manufacturer: detail.Manufacturer,
			Product:      detail.Product,
		})
	}
	return result
}

func Candidates(all []Info, filter Filter) []Info {
	result := make([]Info, 0, len(all))
	for _, port := range all {
		if Matches(port, filter) {
			result = append(result, port)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := score(result[i], filter), score(result[j], filter)
		if left != right {
			return left > right
		}
		return portSortKey(result[i].Name) < portSortKey(result[j].Name)
	})
	return result
}

func Matches(port Info, filter Filter) bool {
	if filter.Port != "" && !strings.EqualFold(filter.Port, port.Name) {
		return false
	}
	if filter.VID != "" && normalizeID(filter.VID) != normalizeID(port.VID) {
		return false
	}
	if filter.PID != "" && normalizeID(filter.PID) != normalizeID(port.PID) {
		return false
	}
	if filter.Name != "" {
		needle := strings.ToLower(filter.Name)
		haystack := strings.ToLower(strings.Join([]string{
			port.Name,
			port.Product,
			port.FriendlyName,
			port.Manufacturer,
			port.SerialNumber,
		}, "\x00"))
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	if filter.SerialNumber != "" &&
		!strings.EqualFold(
			strings.TrimSpace(filter.SerialNumber),
			strings.TrimSpace(port.SerialNumber),
		) {
		return false
	}
	if filter.InstanceID != "" &&
		!strings.EqualFold(
			strings.TrimSpace(filter.InstanceID),
			strings.TrimSpace(port.InstanceID),
		) {
		return false
	}
	return true
}

func (info Info) Label() string {
	identity := strings.TrimSpace(info.FriendlyName)
	if identity == "" {
		identity = strings.TrimSpace(strings.Join(
			[]string{info.Manufacturer, info.Product},
			" ",
		))
	}
	if identity == "" {
		identity = "serial"
	}
	label := fmt.Sprintf("%s  %s", info.Name, identity)
	if info.VID != "" || info.PID != "" {
		label += fmt.Sprintf("  VID:%s PID:%s", info.VID, info.PID)
	}
	if info.SerialNumber != "" {
		label += "  SN:" + info.SerialNumber
	}
	if info.InstanceID != "" {
		label += "  INSTANCE:" + info.InstanceID
	}
	return label
}

func score(port Info, filter Filter) int {
	result := 0
	if filter.Port != "" && strings.EqualFold(filter.Port, port.Name) {
		result += 100
	}
	if filter.VID != "" && normalizeID(filter.VID) == normalizeID(port.VID) {
		result += 20
	}
	if filter.PID != "" && normalizeID(filter.PID) == normalizeID(port.PID) {
		result += 20
	}
	if filter.Name != "" && Matches(port, Filter{Name: filter.Name}) {
		result += 10
	}
	preferred := filter.Preferred
	if preferred.InstanceID != "" &&
		strings.EqualFold(preferred.InstanceID, port.InstanceID) {
		result += 1000
	}
	if preferred.SerialNumber != "" &&
		strings.EqualFold(preferred.SerialNumber, port.SerialNumber) {
		result += 800
	}
	if preferred.VID != "" &&
		normalizeID(preferred.VID) == normalizeID(port.VID) {
		result += 30
	}
	if preferred.PID != "" &&
		normalizeID(preferred.PID) == normalizeID(port.PID) {
		result += 30
	}
	if preferred.Name != "" && stableNameMatches(preferred.Name, port) {
		result += 20
	}
	if preferred.Port != "" && strings.EqualFold(preferred.Port, port.Name) {
		result += 5
	}
	if port.IsUSB {
		result++
	}
	return result
}

func classifyHardwareProblem(problem uint32, hardwareIDs []string) string {
	if problem == 43 {
		for _, id := range hardwareIDs {
			normalized := strings.ToUpper(strings.TrimSpace(id))
			if normalized == `USB\DEVICE_DESCRIPTOR_FAILURE` ||
				strings.Contains(normalized, `USB\VID_0000&PID_0002`) {
				return HardwareProblemUSBDescriptorFailure
			}
		}
		return HardwareProblemReported
	}
	switch problem {
	case 10:
		return HardwareProblemCannotStart
	case 22:
		return HardwareProblemDisabled
	case 28:
		return HardwareProblemDriverMissing
	case 31:
		return HardwareProblemDriverFailure
	case 47:
		return HardwareProblemRemovalPending
	default:
		return HardwareProblemUnknown
	}
}

func hardwareProblemMatches(
	deviceID string,
	hardwareIDs []string,
	filter Filter,
	relatedInstanceIDs []string,
	relatedIdentityAmbiguous bool,
) bool {
	strongIdentities := []string{filter.InstanceID, filter.Preferred.InstanceID}
	haveStrongIdentity := false
	for _, identity := range strongIdentities {
		if strings.TrimSpace(identity) == "" {
			continue
		}
		haveStrongIdentity = true
		if sameDeviceIdentity(deviceID, identity) {
			return true
		}
	}
	// Once a selected or authenticated instance exists, neither a stale COM
	// registry assignment nor an identical sibling's VID/PID may override it.
	if haveStrongIdentity {
		return false
	}
	// A COM name can be reassigned over time. If its registry history names
	// multiple physical instance tails, none of them is reliable enough to blame
	// during a descriptor failure and VID/PID fallback is equally unsafe.
	if relatedIdentityAmbiguous {
		return false
	}
	for _, identity := range relatedInstanceIDs {
		if sameDeviceIdentity(deviceID, identity) {
			return true
		}
	}
	// Historical instances tied to a configured COM port are stronger than a
	// generic model ID. If none matched, do not blame a same-model sibling.
	if len(relatedInstanceIDs) != 0 {
		return false
	}
	vid := firstNonEmpty(filter.VID, filter.Preferred.VID)
	pid := firstNonEmpty(filter.PID, filter.Preferred.PID)
	if vid == "" || pid == "" {
		return false
	}
	needle := "VID_" + normalizeID(vid) + "&PID_" + normalizeID(pid)
	for _, id := range append([]string{deviceID}, hardwareIDs...) {
		if strings.Contains(strings.ToUpper(id), needle) {
			return true
		}
	}
	return false
}

func sameDeviceIdentity(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if strings.EqualFold(left, right) {
		return true
	}
	leftTail, rightTail := deviceInstanceTail(left), deviceInstanceTail(right)
	return leftTail != "" && rightTail != "" && strings.EqualFold(leftTail, rightTail)
}

func deviceInstanceTail(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndex(value, `\`); index >= 0 {
		return strings.TrimSpace(value[index+1:])
	}
	return ""
}

func deviceIdentityHistoryAmbiguous(instanceIDs []string) bool {
	tails := make(map[string]bool)
	for _, instanceID := range instanceIDs {
		tail := strings.ToUpper(deviceInstanceTail(instanceID))
		if tail == "" {
			return len(instanceIDs) > 1
		}
		tails[tail] = true
	}
	return len(tails) > 1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func hardwareProblemLess(left, right HardwareProblem) bool {
	severityRank := func(value string) int {
		if strings.EqualFold(value, "error") {
			return 0
		}
		return 1
	}
	if leftRank, rightRank := severityRank(left.Severity), severityRank(right.Severity); leftRank != rightRank {
		return leftRank < rightRank
	}
	if left.Code != right.Code {
		return left.Code < right.Code
	}
	return left.DeviceID < right.DeviceID
}

// PreferredCandidate returns a prior stable identity only when exactly one
// current candidate matches its serial number or PnP instance. A remembered
// COM number alone is never considered stable enough to suppress ambiguity.
func PreferredCandidate(candidates []Info, preferred Identity) (Info, bool) {
	if len(candidates) == 1 {
		return candidates[0], true
	}
	var matches []Info
	for _, candidate := range candidates {
		switch {
		case preferred.SerialNumber != "" &&
			strings.EqualFold(preferred.SerialNumber, candidate.SerialNumber):
			matches = append(matches, candidate)
		case preferred.InstanceID != "" &&
			strings.EqualFold(preferred.InstanceID, candidate.InstanceID):
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return Info{}, false
}

// ReconnectCandidates relaxes only a stale COM-name constraint after an
// authenticated USB transport disappears. Strong serial/PnP identity wins
// when it survives the move. Bridges such as CH340 often expose neither, so a
// unique VID/PID/friendly-name match may rebind to the new COM assignment.
// Ambiguous matches are returned unchanged and remain subject to the ordinary
// explicit-selection rule; an arbitrary serial device is never guessed.
func ReconnectCandidates(all []Info, filter Filter) []Info {
	if strings.TrimSpace(filter.Port) == "" {
		return nil
	}

	base := filter
	base.Port = ""
	if base.SerialNumber == "" && base.InstanceID == "" {
		if serial := strings.TrimSpace(filter.Preferred.SerialNumber); serial != "" {
			strong := base
			strong.SerialNumber = serial
			if candidates := Candidates(all, strong); len(candidates) != 0 {
				return candidates
			}
		}
		if instance := strings.TrimSpace(filter.Preferred.InstanceID); instance != "" {
			strong := base
			strong.InstanceID = instance
			if candidates := Candidates(all, strong); len(candidates) != 0 {
				return candidates
			}
		}
	}

	if base.VID != "" || base.PID != "" || base.Name != "" ||
		base.SerialNumber != "" || base.InstanceID != "" {
		if candidates := Candidates(all, base); len(candidates) != 0 {
			return candidates
		}
	}

	// Moving a non-serialized bridge can change its Windows instance ID. Retain
	// descriptive identity from the last authenticated transport and relax only
	// the identifiers that are known to change with the physical USB path.
	loose := base
	loose.SerialNumber = ""
	loose.InstanceID = ""
	if loose.VID == "" {
		loose.VID = filter.Preferred.VID
	}
	if loose.PID == "" {
		loose.PID = filter.Preferred.PID
	}
	if loose.Name == "" {
		loose.Name = filter.Preferred.Name
	}
	if loose.VID == "" && loose.PID == "" && loose.Name == "" {
		return nil
	}
	return Candidates(all, loose)
}

// ParseSelector accepts a COM device ID, tcp endpoint, VID:PID pair,
// VID_xxxx&PID_yyyy token, serial:VALUE, instance:VALUE, or a human-friendly
// name substring.
func ParseSelector(value string) (Filter, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Filter{}, errors.New("device selector is empty")
	}
	upper := strings.ToUpper(value)
	if strings.HasPrefix(strings.ToLower(value), "tcp://") ||
		isCOMName(upper) {
		return Filter{Port: value}, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "serial:") {
		serial := strings.TrimSpace(value[len("serial:"):])
		if serial == "" {
			return Filter{}, errors.New("serial selector value is empty")
		}
		return Filter{SerialNumber: serial}, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "instance:") {
		instance := strings.TrimSpace(value[len("instance:"):])
		if instance == "" {
			return Filter{}, errors.New("instance selector value is empty")
		}
		return Filter{InstanceID: instance}, nil
	}
	if vid, pid, ok := parseVIDPID(value); ok {
		return Filter{VID: vid, PID: pid}, nil
	}
	return Filter{Name: value}, nil
}

func Merge(base, override Filter) Filter {
	if override.Port != "" {
		base.Port = override.Port
	}
	if override.VID != "" {
		base.VID = override.VID
	}
	if override.PID != "" {
		base.PID = override.PID
	}
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.SerialNumber != "" {
		base.SerialNumber = override.SerialNumber
	}
	if override.InstanceID != "" {
		base.InstanceID = override.InstanceID
	}
	return base
}

func parseVIDPID(value string) (string, string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.Contains(upper, "VID_") && strings.Contains(upper, "PID_") {
		vidAt := strings.Index(upper, "VID_") + 4
		pidAt := strings.Index(upper, "PID_") + 4
		vid := takeHex(upper[vidAt:])
		pid := takeHex(upper[pidAt:])
		if vid != "" && pid != "" {
			return vid, pid, true
		}
	}
	parts := strings.FieldsFunc(upper, func(r rune) bool {
		return r == ':' || r == '/' || r == ',' || r == ';'
	})
	if len(parts) == 2 {
		vid := strings.TrimPrefix(strings.TrimSpace(parts[0]), "VID=")
		pid := strings.TrimPrefix(strings.TrimSpace(parts[1]), "PID=")
		if isHexID(vid) && isHexID(pid) {
			return vid, pid, true
		}
	}
	return "", "", false
}

func takeHex(value string) string {
	end := 0
	for end < len(value) {
		char := value[end]
		if !((char >= '0' && char <= '9') ||
			(char >= 'A' && char <= 'F')) {
			break
		}
		end++
	}
	return value[:end]
}

func isHexID(value string) bool {
	value = strings.TrimPrefix(value, "0X")
	if value == "" || len(value) > 4 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') ||
			(char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func isCOMName(value string) bool {
	if !strings.HasPrefix(value, "COM") || len(value) == 3 {
		return false
	}
	_, err := strconv.Atoi(value[3:])
	return err == nil
}

func stableNameMatches(value string, port Info) bool {
	needle := strings.ToLower(stableFriendlyName(value))
	for _, candidate := range []string{
		port.FriendlyName,
		port.Product,
	} {
		if strings.Contains(
			strings.ToLower(stableFriendlyName(candidate)),
			needle,
		) {
			return true
		}
	}
	return false
}

func stableFriendlyName(value string) string {
	value = strings.TrimSpace(value)
	upper := strings.ToUpper(value)
	if end := strings.LastIndex(upper, "(COM"); end >= 0 &&
		strings.HasSuffix(upper, ")") {
		if _, err := strconv.Atoi(upper[end+4 : len(upper)-1]); err == nil {
			value = strings.TrimSpace(value[:end])
		}
	}
	return value
}

func normalizeID(value string) string {
	value = strings.TrimSpace(strings.ToUpper(value))
	value = strings.TrimPrefix(value, "0X")
	return strings.TrimLeft(value, "0")
}

func portSortKey(name string) string {
	upper := strings.ToUpper(name)
	if strings.HasPrefix(upper, "COM") {
		if number, err := strconv.Atoi(strings.TrimPrefix(upper, "COM")); err == nil {
			return fmt.Sprintf("COM%08d", number)
		}
	}
	return upper
}
