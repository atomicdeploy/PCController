package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/native"
)

const menuUsage = "menu list|current|layout [reset|set MASK PAGE...]|show PAGE|hide PAGE|move PAGE RANK|order PAGE...|page PAGE|prev|next|dec|inc"

type MenuPageInfo struct {
	ID          byte   `json:"id"`
	Key         string `json:"key"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type MenuCatalog struct {
	Source       string         `json:"source"`
	LiveList     bool           `json:"live_list"`
	FirmwareHash uint32         `json:"firmware_hash"`
	CurrentPage  byte           `json:"current_page"`
	ProgramMode  byte           `json:"program_mode"`
	Pages        []MenuPageInfo `json:"pages"`
	Layout       MenuLayout     `json:"layout"`
}

var protocolMenuPages = []MenuPageInfo{
	{0, "door", "door", "Door", "Enclosure reed-switch state: OPEN or CLSd"},
	{1, "voltage", "VOLT", "Voltage", "Supply and INA219 bus voltage"},
	{2, "current", "CURR", "Current", "INA219 current and power"},
	{3, "temperature-led", "tLED", "LED Temperature", "Illumination temperature sensor"},
	{4, "temperature-bt", "t-bt", "BT Audio Temperature", "BT-5.0-Pro Audio temperature sensor"},
	{5, "illumination", "LItE", "Illumination", "Enclosure illumination mode and levels"},
	{6, "settings", "bEEP", "Beep and Settings", "Beep, display, status color, and decimals"},
	{7, "pwm-test", "PWM", "PWM Test", "PWM output test and channel selection"},
	{8, "relay-test", "rELY", "Relay Test", "Individual relay output test"},
	{9, "keys", "KEY", "Motion Keys", "K1/K2 control Side A; K3/K4 control Side B"},
	{10, "user-pwm", "uPWM", "User PWM", "Eight user MOSFET output values"},
	{11, "user-relays", "r5-8", "User Relays", "R5 through R8 toggle or momentary control"},
	{12, "motion", "MOVE", "Motion", "Side A and Side B direction/output control"},
	{13, "rf-learn", "LErn", "RF Learn", "Learn and map 433 MHz controls"},
}

const voltageFirstMenuBuildHash uint32 = 0x5DF10D05

// voltageFirstMenuPages is the frozen directory for the verified pre-MENU_LIST
// build above. Unknown builds must not inherit these historical numeric IDs:
// without an exact identity match, the current catalog remains the safe host
// fallback.
var voltageFirstMenuPages = []MenuPageInfo{
	{0, "voltage", "VOLT", "Voltage", "Supply and INA219 bus voltage"},
	{1, "current", "CURR", "Current", "INA219 current and power"},
	{2, "temperature-led", "tLED", "LED Temperature", "Illumination temperature sensor"},
	{3, "temperature-bt", "t-bt", "BT Audio Temperature", "BT-5.0-Pro Audio temperature sensor"},
	{4, "illumination", "LItE", "Illumination", "Enclosure illumination mode and levels"},
	{5, "bt-audio", "bt", "BT Audio", "BT-5.0-Pro Audio indicator state"},
	{6, "settings", "Snd", "Sound and Settings", "Sound, display, status color, and decimals"},
	{7, "pwm-test", "PWM", "PWM Test", "PWM output test and channel selection"},
	{8, "relay-test", "rELY", "Relay Test", "Individual relay output test"},
	{9, "keys", "KEY", "Key Identification", "Identify K1 through K4"},
	{10, "user-pwm", "uPWM", "User PWM", "Eight user MOSFET output values"},
	{11, "user-relays", "r5-8", "User Relays", "R5 through R8 toggle or momentary control"},
	{12, "motion", "MOVE", "Motion", "Side A and Side B direction/output control"},
	{13, "rf-learn", "LErn", "RF Learn", "Learn and map 433 MHz controls"},
	{14, "status", "STAT", "Status", "Incoming events and controller status"},
}

func MenuPages() []MenuPageInfo {
	return append([]MenuPageInfo(nil), protocolMenuPages...)
}

// MenuPagesForCapabilities returns the canonical current catalog. Capability
// differences may remove live discovery, but never resurrect retired pages.
func MenuPagesForCapabilities(capabilities uint32) []MenuPageInfo {
	_ = capabilities
	return MenuPages()
}

// menuPagesForHello selects a host fallback only when the board cannot
// advertise its own directory. An advertised directory always identifies the
// current generation; a historical layout is used only for an exact, stable
// compact HELLO build identity.
func menuPagesForHello(hello native.Hello) []MenuPageInfo {
	if hello.Capabilities&native.CapabilityMenuDirectory != 0 {
		return MenuPages()
	}
	if hello.IdentitySchema == native.IdentitySchemaCompact &&
		hello.BuildHash == voltageFirstMenuBuildHash {
		return append([]MenuPageInfo(nil), voltageFirstMenuPages...)
	}
	return MenuPages()
}

func ResolveMenuPage(reference string) (MenuPageInfo, error) {
	return ResolveMenuPageIn(protocolMenuPages, reference)
}

// ResolveMenuPageIn resolves names and IDs against the connected board's
// active catalog instead of assuming a firmware generation's numeric layout.
func ResolveMenuPageIn(pages []MenuPageInfo, reference string) (MenuPageInfo, error) {
	reference = strings.TrimSpace(reference)
	if value, err := strconv.ParseUint(reference, 0, 8); err == nil {
		for _, page := range pages {
			if page.ID == byte(value) {
				return page, nil
			}
		}
		return MenuPageInfo{}, fmt.Errorf("menu page %d is not in the protocol manifest", value)
	}
	normalized := normalizeMenuName(reference)
	var matches []MenuPageInfo
	for _, page := range pages {
		for _, candidate := range []string{page.Key, page.Name, page.Label} {
			candidate = normalizeMenuName(candidate)
			if candidate == normalized {
				return page, nil
			}
			if strings.HasPrefix(candidate, normalized) {
				matches = append(matches, page)
				break
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return MenuPageInfo{}, fmt.Errorf("menu page %q is ambiguous", reference)
	}
	return MenuPageInfo{}, fmt.Errorf("unknown menu page %q", reference)
}

// QueryMenuCatalog returns the board-owned directory when advertised and the
// generation-correct immutable fallback for older firmware.
func QueryMenuCatalog(ctx context.Context, runtime *Runtime) (MenuCatalog, error) {
	status, err := runtime.RefreshStatus(ctx)
	if err != nil {
		return MenuCatalog{}, err
	}
	snapshot := runtime.Snapshot()
	capabilities := snapshot.Hello.Capabilities
	var catalog MenuCatalog
	if capabilities&native.CapabilityMenuDirectory != 0 {
		catalog, err = queryLiveMenuCatalog(ctx, runtime, status)
	} else {
		pages := menuPagesForHello(snapshot.Hello)
		source := "host current manifest (firmware has no MENU_LIST capability)"
		if snapshot.Hello.IdentitySchema == native.IdentitySchemaCompact &&
			snapshot.Hello.BuildHash == voltageFirstMenuBuildHash {
			source = "host build-identity compatibility manifest (firmware has no MENU_LIST capability)"
		}
		catalog = MenuCatalog{
			Source:       source,
			LiveList:     false,
			FirmwareHash: snapshot.Hello.BuildHash,
			CurrentPage:  status.MenuPage,
			ProgramMode:  status.ProgramMode,
			Pages:        pages,
		}
	}
	if err != nil {
		return MenuCatalog{}, err
	}
	catalog.Layout, err = queryMenuLayoutForCatalog(ctx, runtime, catalog.Pages, capabilities)
	if err != nil {
		return MenuCatalog{}, err
	}
	catalog.Pages, err = OrderedMenuPages(catalog.Pages, catalog.Layout)
	if err != nil {
		return MenuCatalog{}, err
	}
	return catalog, nil
}

func queryMenuLayoutForCatalog(
	ctx context.Context,
	runtime *Runtime,
	pages []MenuPageInfo,
	capabilities uint32,
) (MenuLayout, error) {
	if capabilities&native.CapabilityMenuLayout == 0 {
		return DefaultMenuLayout(pages)
	}
	frame, err := request(ctx, runtime, native.OpMenuLayoutGet, nil, native.OpMenuLayoutResp)
	if err != nil {
		return MenuLayout{}, err
	}
	decoded, err := native.ParseMenuLayout(frame.Payload)
	if err != nil {
		return MenuLayout{}, err
	}
	return CanonicalMenuLayout(pages, MenuLayout{
		Schema: decoded.Schema, Supported: true, Persistent: true,
		Source: "board EEPROM MENU_LAYOUT", VisibleMask: decoded.VisibleMask,
		Order: decoded.Order,
	})
}

// QueryMenuLayout returns the same capability-gated, catalog-validated layout
// used by MenuCatalog. Older firmware receives an explicit read-only fallback.
func QueryMenuLayout(ctx context.Context, runtime *Runtime) (MenuLayout, error) {
	catalog, err := QueryMenuCatalog(ctx, runtime)
	if err != nil {
		return MenuLayout{}, err
	}
	return catalog.Layout, nil
}

// PersistMenuLayout writes the board-owned EEPROM layout only when capability
// bit 23 is advertised, then performs an exact GET/readback comparison.
func PersistMenuLayout(ctx context.Context, runtime *Runtime, requested MenuLayout) (MenuLayout, error) {
	if runtime.Snapshot().Hello.Capabilities&native.CapabilityMenuLayout == 0 {
		return MenuLayout{}, ErrMenuLayoutUnsupported
	}
	catalog, err := QueryMenuCatalog(ctx, runtime)
	if err != nil {
		return MenuLayout{}, err
	}
	requested, err = CanonicalMenuLayout(catalog.Pages, requested)
	if err != nil {
		return MenuLayout{}, err
	}
	payload, err := native.EncodeMenuLayout(native.MenuLayout{
		Schema: native.MenuLayoutSchema, VisibleMask: requested.VisibleMask,
		Order: requested.Order,
	})
	if err != nil {
		return MenuLayout{}, err
	}
	if err := command(ctx, runtime, native.OpMenuLayoutSet, payload); err != nil {
		return MenuLayout{}, err
	}
	verified, err := queryMenuLayoutForCatalog(
		ctx, runtime, catalog.Pages, native.CapabilityMenuLayout,
	)
	if err != nil {
		return MenuLayout{}, fmt.Errorf("read back persisted menu layout: %w", err)
	}
	if verified.VisibleMask != requested.VisibleMask || !sameMenuOrder(verified.Order, requested.Order) {
		return MenuLayout{}, fmt.Errorf(
			"menu-layout readback mismatch: wrote mask=0x%04X order=%v, read mask=0x%04X order=%v",
			requested.VisibleMask, requested.Order, verified.VisibleMask, verified.Order,
		)
	}
	return verified, nil
}

func sameMenuOrder(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeMenuName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value)
}

func runMenuCommand(ctx context.Context, runtime *Runtime, args []string) (string, error) {
	if len(args) == 1 && (strings.EqualFold(args[0], "list") ||
		strings.EqualFold(args[0], "current") || strings.EqualFold(args[0], "layout")) {
		catalog, err := QueryMenuCatalog(ctx, runtime)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(args[0]) {
		case "current":
			page, resolveErr := menuPageByID(catalog.Pages, catalog.CurrentPage)
			if resolveErr != nil {
				return fmt.Sprintf("menu page=%d mode=%d (unknown to active catalog)", catalog.CurrentPage, catalog.ProgramMode), nil
			}
			return fmt.Sprintf("menu page=%d key=%s name=%q label=%q mode=%d", page.ID, page.Key, page.Name, page.Label, catalog.ProgramMode), nil
		case "layout":
			return formatMenuLayout(catalog), nil
		default:
			return formatMenuCatalog(catalog), nil
		}
	}
	if len(args) == 2 && (strings.EqualFold(args[0], "page") || strings.EqualFold(args[0], "jump")) {
		catalog, err := QueryMenuCatalog(ctx, runtime)
		if err != nil {
			return "", err
		}
		page, err := ResolveMenuPageIn(catalog.Pages, args[1])
		if err != nil {
			return "", err
		}
		if err := command(ctx, runtime, native.OpMenuSetPage, []byte{page.ID}); err != nil {
			return "", err
		}
		return fmt.Sprintf("menu page %d/%s selected (%s)", page.ID, page.Key, page.Name), nil
	}
	if len(args) == 2 && (strings.EqualFold(args[0], "show") || strings.EqualFold(args[0], "hide")) {
		catalog, err := QueryMenuCatalog(ctx, runtime)
		if err != nil {
			return "", err
		}
		page, err := ResolveMenuPageIn(catalog.Pages, args[1])
		if err != nil {
			return "", err
		}
		layout, err := SetMenuPageVisible(catalog.Pages, catalog.Layout, page.ID, strings.EqualFold(args[0], "show"))
		if err != nil {
			return "", err
		}
		stored, err := PersistMenuLayout(ctx, runtime, layout)
		if err != nil {
			return "", err
		}
		catalog.Layout = stored
		return formatMenuLayout(catalog), nil
	}
	if len(args) == 3 && strings.EqualFold(args[0], "move") {
		catalog, err := QueryMenuCatalog(ctx, runtime)
		if err != nil {
			return "", err
		}
		page, err := ResolveMenuPageIn(catalog.Pages, args[1])
		if err != nil {
			return "", err
		}
		rank, err := strconv.Atoi(args[2])
		if err != nil {
			return "", fmt.Errorf("menu rank %q is not an integer", args[2])
		}
		layout, err := MoveMenuPage(catalog.Pages, catalog.Layout, page.ID, rank)
		if err != nil {
			return "", err
		}
		stored, err := PersistMenuLayout(ctx, runtime, layout)
		if err != nil {
			return "", err
		}
		catalog.Layout = stored
		return formatMenuLayout(catalog), nil
	}
	if len(args) >= 2 && strings.EqualFold(args[0], "order") {
		return storeMenuOrder(ctx, runtime, args[1:])
	}
	if len(args) == 2 && strings.EqualFold(args[0], "layout") && strings.EqualFold(args[1], "reset") {
		catalog, err := QueryMenuCatalog(ctx, runtime)
		if err != nil {
			return "", err
		}
		layout, err := DefaultMenuLayout(catalog.Pages)
		if err != nil {
			return "", err
		}
		stored, err := PersistMenuLayout(ctx, runtime, layout)
		if err != nil {
			return "", err
		}
		catalog.Layout = stored
		return formatMenuLayout(catalog), nil
	}
	if len(args) >= 4 && strings.EqualFold(args[0], "layout") && strings.EqualFold(args[1], "set") {
		mask, err := strconv.ParseUint(args[2], 0, 16)
		if err != nil {
			return "", fmt.Errorf("menu visibility mask %q is not a 16-bit integer", args[2])
		}
		return storeExplicitMenuLayout(ctx, runtime, uint16(mask), args[3:])
	}
	if len(args) == 1 {
		actions := map[string]byte{
			"prev": native.MenuPrevious, "previous": native.MenuPrevious,
			"next": native.MenuNext, "dec": native.MenuDecrease,
			"decrease": native.MenuDecrease, "inc": native.MenuIncrease,
			"increase": native.MenuIncrease,
		}
		if action, ok := actions[strings.ToLower(args[0])]; ok {
			if err := command(ctx, runtime, native.OpMenuAction, []byte{action}); err != nil {
				return "", err
			}
			return "menu " + strings.ToLower(args[0]), nil
		}
	}
	return "", fmt.Errorf("usage: %s", menuUsage)
}

func storeMenuOrder(ctx context.Context, runtime *Runtime, references []string) (string, error) {
	catalog, err := QueryMenuCatalog(ctx, runtime)
	if err != nil {
		return "", err
	}
	return storeResolvedMenuLayout(ctx, runtime, catalog, catalog.Layout.VisibleMask, references)
}

func storeExplicitMenuLayout(ctx context.Context, runtime *Runtime, mask uint16, references []string) (string, error) {
	catalog, err := QueryMenuCatalog(ctx, runtime)
	if err != nil {
		return "", err
	}
	return storeResolvedMenuLayout(ctx, runtime, catalog, mask, references)
}

func storeResolvedMenuLayout(ctx context.Context, runtime *Runtime, catalog MenuCatalog, mask uint16, references []string) (string, error) {
	order := make([]byte, 0, len(references))
	for _, reference := range references {
		page, err := ResolveMenuPageIn(catalog.Pages, reference)
		if err != nil {
			return "", err
		}
		order = append(order, page.ID)
	}
	layout, err := CanonicalMenuLayout(catalog.Pages, MenuLayout{VisibleMask: mask, Order: order})
	if err != nil {
		return "", err
	}
	stored, err := PersistMenuLayout(ctx, runtime, layout)
	if err != nil {
		return "", err
	}
	catalog.Layout = stored
	return formatMenuLayout(catalog), nil
}

func formatMenuLayout(catalog MenuCatalog) string {
	lines := []string{fmt.Sprintf(
		"menu layout schema=%d supported=%t persistent=%t mask=0x%04X source=%s",
		catalog.Layout.Schema, catalog.Layout.Supported, catalog.Layout.Persistent,
		catalog.Layout.VisibleMask, catalog.Layout.Source,
	)}
	for rank, id := range catalog.Layout.Order {
		page, err := menuPageByID(catalog.Pages, id)
		if err != nil {
			lines = append(lines, fmt.Sprintf("rank=%d visible=%t id=%d unknown", rank, catalog.Layout.Visible(id), id))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"rank=%d visible=%t id=%d key=%s label=%q name=%q",
			rank, catalog.Layout.Visible(id), id, page.Key, page.Label, page.Name,
		))
	}
	return strings.Join(lines, "\n")
}
