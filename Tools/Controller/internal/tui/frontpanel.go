package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

const (
	segA byte = 1 << iota
	segB
	segC
	segD
	segE
	segF
	segG
)

func (model Model) currentFrontPanel(snapshot control.Snapshot) FrontPanelState {
	var state FrontPanelState
	if model.hostMenus != nil && model.hostMenus.Snapshot().Active {
		state = hostMenuPanelState(model.hostMenus.Snapshot())
	} else if model.frontPanel != nil {
		state = model.frontPanel()
	} else if frontPanelSnapshotAvailable(snapshot) {
		panel := snapshot.FrontPanel
		state = FrontPanelState{
			RawSegments: panel.RawSegments, HasRawSegments: true,
			Blink: panel.Blink, CategorySelector: panel.CategorySelector,
			Brightness: panel.Brightness,
			MenuID:     panel.MenuPage,
			MenuName:   model.menuPageByID(panel.MenuPage).Name,
			Submode:    model.programModeName(panel.ProgramMode), PressedKeys: panel.PressedKeys,
			InputSource: "FRONT_PANEL schema " + fmt.Sprint(panel.Schema), Exact: true,
		}
		if panel.HostCaptured {
			state.MenuName = "Host-captured front panel"
			state.Submode = fmt.Sprintf("host state=%d value=%d", panel.HostState, panel.HostEditableValue)
		}
	} else if model.preview != nil {
		state = model.previewPanel
	} else {
		page := model.menuPageByID(snapshot.Status.MenuPage)
		brightness := snapshot.Settings.DisplayBrightness
		if !snapshot.Status.DoorOpen {
			brightness = snapshot.Settings.DisplayClosedBrightness
		}
		state = FrontPanelState{
			Segments: page.Short, Brightness: brightness,
			LCDBacklight: snapshot.Settings.Flags&2 != 0,
			MenuID:       snapshot.Status.MenuPage, MenuName: page.Name,
			Submode:     model.programModeName(snapshot.Status.ProgramMode),
			PressedKeys: snapshot.Status.ActiveKeys, InputSource: "STATUS summary",
			Exact: false,
		}
		if snapshot.Connected && model.lcdDisplayAvailable(snapshot) {
			state.LCDLine1 = page.Name
			state.LCDLine2 = state.Submode
		}
		if snapshot.HaveFrontPanelSegments {
			state.RawSegments = snapshot.FrontPanel.RawSegments
			state.HasRawSegments = true
			state.Brightness = snapshot.FrontPanel.Brightness
			state.InputSource = "SEGMENT_CHANGED + STATUS summary"
		}
	}
	lcdPresentation, haveLCDPresentation := model.currentLCDPresentation(snapshot)
	lcdAddress, haveLCD := model.lcdDisplayState(snapshot)
	state.HaveLCD = haveLCD
	if haveLCD {
		switch {
		case haveLCDPresentation && lcdPresentation.Physical:
			state.LCDLine1 = lcdPresentation.PhysicalLine1
			state.LCDLine2 = lcdPresentation.PhysicalLine2
			state.LCDBacklight = true
		case frontPanelSnapshotAvailable(snapshot) && snapshot.FrontPanel.LCDAvailable:
			state.LCDLine1 = snapshot.FrontPanel.LCDLine1
			state.LCDLine2 = snapshot.FrontPanel.LCDLine2
			state.LCDBacklight = snapshot.FrontPanel.LCDBacklight
		case haveLCDPresentation && lcdPresentation.FirmwareMirror:
			state.LCDLine1 = lcdPresentation.FirmwareLine1
			state.LCDLine2 = lcdPresentation.FirmwareLine2
		}
		state.InputSource += fmt.Sprintf(" · LCD 0x%02X", lcdAddress)
	}
	if model.lcdMirror && haveLCD {
		switch {
		case model.preview != nil:
			line1 := model.input.Value()
			if line1 == "" {
				line1 = model.input.Placeholder
			}
			line2 := model.notice
			if len(model.completion) != 0 {
				line2 = model.completion[model.completionIndex%len(model.completion)]
			}
			state.LCDLine1, state.LCDLine2 = line1, line2
		case !snapshot.Connected:
			state.InputSource += " · USB offline; retained physical text unverified"
		case haveLCDPresentation && lcdPresentation.Physical:
			// The cap16 branch above uses only the PCF8574 driver's confirmed cache.
		case haveLCDPresentation && lcdPresentation.FirmwareMirror:
			state.LCDLine1 = lcdPresentation.FirmwareLine1
			state.LCDLine2 = lcdPresentation.FirmwareLine2
		default:
			state.InputSource += " · LCD mirror pending confirmation"
		}
	}
	if !snapshot.Connected && model.preview == nil {
		state.InputSource += " · last board snapshot"
		state.Exact = false
	}
	if time.Now().Before(model.frontOverlayUntil) {
		state.LCDLine1, state.LCDLine2 = model.frontOverlay1, model.frontOverlay2
	}
	if snapshot.HaveStatusLED {
		state.StatusLED = snapshot.StatusLED
		state.HaveStatusLED = true
	}
	state.Segments = padCells(state.Segments, 4)
	state.LCDLine1 = padCells(state.LCDLine1, 16)
	state.LCDLine2 = padCells(state.LCDLine2, 16)
	return state
}

func frontPanelSnapshotAvailable(snapshot control.Snapshot) bool {
	return snapshot.Connected && snapshot.HaveFrontPanel &&
		snapshot.Hello.Capabilities&native.CapabilityFrontPanelSnapshot != 0
}

func (model Model) frontPanelControlsAvailable(snapshot control.Snapshot) bool {
	return model.frontPanelKey != nil && frontPanelSnapshotAvailable(snapshot) &&
		snapshot.Hello.Capabilities&native.CapabilityRemoteKeys != 0
}

func (model Model) lcdPromptMirrorAvailable(snapshot control.Snapshot) bool {
	return model.mirrorLCD != nil && model.lcdDisplayAvailable(snapshot)
}

func (model Model) currentLCDPresentation(snapshot control.Snapshot) (control.LCDPresentationState, bool) {
	if model.preview != nil && frontPanelSnapshotAvailable(snapshot) {
		panel := snapshot.FrontPanel
		return control.LCDPresentationState{
			Enabled: true, Physical: panel.LCDAvailable, Address: panel.LCDAddress,
			PhysicalLine1: panel.LCDLine1, PhysicalLine2: panel.LCDLine2,
		}, true
	}
	if model.remote != nil {
		return model.lcdPresentation, model.haveLCDPresentation
	}
	if model.runtime != nil {
		return model.runtime.LCDPresenter().State(), true
	}
	return control.LCDPresentationState{}, false
}

func renderFrontPanel(state FrontPanelState) string {
	segments := renderSevenSegments(
		state.Segments, state.RawSegments, state.HasRawSegments,
		state.DecimalMask, state.Blink,
	)
	detail := fmt.Sprintf(
		"menu %d · %s\nsubmode · %s\nbrightness %d/7 · blink %s · category %s\ninput %s · keys 0x%X",
		state.MenuID, state.MenuName, state.Submode, state.Brightness,
		boolWord(state.Blink, "ON", "OFF"), boolWord(state.CategorySelector, "SELECT", "page"),
		state.InputSource, state.PressedKeys,
	)
	if state.HaveStatusLED {
		hex := fmt.Sprintf("#%02X%02X%02X", state.StatusLED.Red, state.StatusLED.Green, state.StatusLED.Blue)
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("●")
		detail += fmt.Sprintf("\nstatus %s %s · %s · condition %d", dot, hex, statusEffectName(state.StatusLED.Effect), state.StatusLED.Condition)
	}
	if !state.Exact {
		detail += "\n" + warnStyle.Render("approximate STATUS fallback · exact display-state opcode unavailable")
	}
	cards := []string{cardStyle.Copy().Render(titleStyle.Render("4-DIGIT DISPLAY") + "\n" + segments)}
	if state.HaveLCD {
		lcd := renderLCD(state.LCDLine1, state.LCDLine2, state.LCDBacklight)
		cards = append(cards, cardStyle.Copy().Render(titleStyle.Render("2×16 LCD")+"\n"+lcd))
	}
	cards = append(cards, cardStyle.Copy().Render(titleStyle.Render("FRONT PANEL STATE")+"\n"+detail))
	joined := cards[0]
	for _, card := range cards[1:] {
		joined = lipgloss.JoinHorizontal(lipgloss.Top, joined, " ", card)
	}
	return joined
}

func statusEffectName(effect byte) string {
	switch effect {
	case native.StatusEffectBreathe:
		return "breathe"
	case native.StatusEffectFlash:
		return "flash"
	case native.StatusEffectCycle:
		return "cycle"
	case native.StatusEffectTransition:
		return "transition"
	default:
		return "steady"
	}
}

func renderSevenSegments(
	value string,
	raw [4]byte,
	haveRaw bool,
	decimals byte,
	blink bool,
) string {
	characters := []rune(padCells(value, 4))
	rows := make([]string, 5)
	for index, character := range characters[:4] {
		mask := sevenSegmentMask(character)
		if haveRaw {
			mask = raw[index] & 0x7F
		}
		if blink {
			mask = 0
		}
		decimal := decimals&(1<<index) != 0 ||
			(haveRaw && raw[index]&0x80 != 0)
		glyph := segmentGlyph(mask, decimal)
		for row := range rows {
			if rows[row] != "" {
				rows[row] += " "
			}
			rows[row] += glyph[row]
		}
	}
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(strings.Join(rows, "\n"))
}

func segmentGlyph(mask byte, decimal bool) [5]string {
	horizontal := func(segment byte) string {
		if mask&segment != 0 {
			return " ━━━ "
		}
		return "     "
	}
	vertical := func(left, right byte) string {
		l, r := " ", " "
		if mask&left != 0 {
			l = "┃"
		}
		if mask&right != 0 {
			r = "┃"
		}
		return l + "   " + r
	}
	bottom := horizontal(segD)
	if decimal {
		bottom = bottom[:4] + "●"
	}
	return [5]string{
		horizontal(segA), vertical(segF, segB), horizontal(segG),
		vertical(segE, segC), bottom,
	}
}

func sevenSegmentMask(character rune) byte {
	switch character {
	case '0', 'O', 'o':
		return segA | segB | segC | segD | segE | segF
	case '1', 'I', 'i':
		return segB | segC
	case '2':
		return segA | segB | segD | segE | segG
	case '3':
		return segA | segB | segC | segD | segG
	case '4':
		return segB | segC | segF | segG
	case '5', 'S', 's':
		return segA | segC | segD | segF | segG
	case '6':
		return segA | segC | segD | segE | segF | segG
	case '7':
		return segA | segB | segC
	case '8':
		return segA | segB | segC | segD | segE | segF | segG
	case '9':
		return segA | segB | segC | segD | segF | segG
	case 'A', 'a':
		return segA | segB | segC | segE | segF | segG
	case 'B', 'b':
		return segC | segD | segE | segF | segG
	case 'C', 'c':
		return segA | segD | segE | segF
	case 'D', 'd':
		return segB | segC | segD | segE | segG
	case 'E', 'e':
		return segA | segD | segE | segF | segG
	case 'F', 'f':
		return segA | segE | segF | segG
	case 'H', 'h':
		return segB | segC | segE | segF | segG
	case 'L', 'l':
		return segD | segE | segF
	case 'N', 'n':
		return segC | segE | segG
	case 'P', 'p':
		return segA | segB | segE | segF | segG
	case 'R', 'r':
		return segE | segG
	case 'T', 't':
		return segD | segE | segF | segG
	case 'U', 'u':
		return segB | segC | segD | segE | segF
	case '-':
		return segG
	default:
		return 0
	}
}

func renderLCD(line1, line2 string, backlight bool) string {
	style := lipgloss.NewStyle().Foreground(colorGood)
	if !backlight {
		style = labelStyle
	}
	return style.Render("╔════════════════╗\n║" + padCells(line1, 16) + "║\n║" + padCells(line2, 16) + "║\n╚════════════════╝")
}

func padCells(value string, width int) string {
	runes := []rune(value)
	if len(runes) > width {
		runes = runes[:width]
	}
	for len(runes) < width {
		runes = append(runes, ' ')
	}
	return string(runes)
}
