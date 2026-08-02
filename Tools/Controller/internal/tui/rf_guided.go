package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

var rfGuideLabels = [...]string{"A", "B", "C", "D"}

func rfGuidedMappingCommandMatches(line string, id int) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	return len(fields) >= 4 && fields[0] == "rf" && fields[1] == "map" && fields[2] == fmt.Sprint(id)
}

func rfGuidedRecordNeedsReview(entry native.RFEntry, entries []native.RFEntry) bool {
	if entry.ActionKind == native.RFActionNone {
		return true
	}
	for _, candidate := range entries {
		if candidate.Code == entry.Code && candidate.Bits == entry.Bits && candidate.Protocol == entry.Protocol {
			return candidate.ID != entry.ID
		}
	}
	return false
}

func (model Model) beginRFGuidedWorkflow() Model {
	model.rfGuideActive = true
	model.rfGuidePhase = "idle"
	model.rfGuideCandidate = nil
	model.rfGuideCandidateCaptured = false
	model.rfGuideAwaitID = -1
	model.rfGuideMappingID = -1
	model.clearRFGuideArms()
	for index, capture := range model.rfGuideCaptures {
		if capture == nil {
			model.rfGuideStep = index
			break
		}
	}
	model.setNotice("Guided RF capture ready for handset button " + rfGuideLabels[model.rfGuideStep])
	return model
}

func (model *Model) clearRFGuideArms() {
	model.rfGuideRemoveArmed = false
	model.rfGuideClearArmed = false
	model.rfGuideTransmitArmed = false
}

func (model Model) startRFGuidedCapture() (Model, tea.Cmd, bool) {
	if model.preview == nil && !model.snapshot().Connected {
		model.rfGuidePhase = "interrupted"
		model.setNotice("Guided capture requires an authenticated controller connection")
		return model, nil, true
	}
	if model.preview == nil && model.runtime.RFLearnState().Active {
		model.rfGuidePhase = "interrupted"
		model.setNotice("Another RF learning session is active; press Esc to cancel it explicitly")
		return model, nil, true
	}
	model.rfGuidePhase = "capturing"
	model.rfGuideCandidate = nil
	model.rfGuideCandidateCaptured = false
	model.rfGuideAwaitID = -1
	model.clearRFGuideArms()
	model.setNotice("Listening for handset button " + rfGuideLabels[model.rfGuideStep] + " for 30 seconds")
	return model.dispatchLine("rf learn timer 30s")
}

func (model *Model) observeRFGuidedEvent(event control.Event) tea.Cmd {
	if !model.rfGuideActive {
		return nil
	}
	if event.Kind == "connection" && event.State == "disconnected" {
		model.rfGuidePhase = "interrupted"
		model.setNotice("Guided RF capture paused because the controller disconnected")
		return nil
	}
	if model.rfGuidePhase == "capturing" {
		switch strings.ToLower(event.Kind) {
		case "rf.learn.ended", "rf.learn.cancelled":
			model.rfGuidePhase = "interrupted"
			model.setNotice("No handset identity was confirmed; press Enter to retry this step")
			return nil
		case "rf.learn.full":
			model.rfGuidePhase = "interrupted"
			model.setNotice("RF storage is full; review stale records before retrying")
			return nil
		}
	}
	if model.rfGuidePhase != "capturing" || event.Kind != "rf.learn.mapping-required" {
		return nil
	}
	id := -1
	if event.HaveRFID {
		id = int(event.RFID)
	} else if event.Frame.Opcode == native.OpEvent {
		if parsed, err := native.ParseDeviceEvent(event.Frame.Payload); err == nil && parsed.Type == native.EventRFLearned {
			id = int(parsed.RFID)
		}
	}
	if id < 0 {
		return nil
	}
	model.rfGuideAwaitID = id
	model.rfGuidePhase = "resolving"
	model.setNotice(fmt.Sprintf("RF entry %d received; verifying exact identity by board readback", id))
	model.rfPending = true
	return tea.Sequence(execute(model.engine, "rf cancel"), model.fetchRFEntriesCommand())
}

func (model *Model) resolveRFGuidedCandidate(entries []native.RFEntry) {
	if !model.rfGuideActive || model.rfGuidePhase != "resolving" || model.rfGuideAwaitID < 0 {
		return
	}
	for index := range entries {
		if int(entries[index].ID) != model.rfGuideAwaitID {
			continue
		}
		entry := entries[index]
		model.rfGuideCandidate = &entry
		model.rfGuideCandidateCaptured = true
		model.rfGuidePhase = "identity"
		model.rfGuideAwaitID = -1
		for cursor := range model.rfStaged {
			if model.rfStaged[cursor].ID == entry.ID {
				model.cursor = cursor
				break
			}
		}
		model.setNotice(fmt.Sprintf(
			"Confirm button %s: ID %d · %s · %d bits · protocol %d · %d us",
			rfGuideLabels[model.rfGuideStep], entry.ID,
			appconfig.FormatRFCode(entry.Code, model.rfValue.DisplayRadix),
			entry.Bits, entry.Protocol, entry.PulseUS,
		))
		return
	}
	model.setNotice(fmt.Sprintf("Captured RF entry %d is not visible yet; waiting for the next board readback", model.rfGuideAwaitID))
}

func (model Model) beginRFGuidedMapping(candidate native.RFEntry, captured bool) Model {
	for index := range model.rfStaged {
		if model.rfStaged[index].ID == candidate.ID {
			model.cursor = index
			break
		}
	}
	entry := candidate
	model.rfGuideCandidate = &entry
	model.rfGuideCandidateCaptured = captured
	model.rfGuidePhase = "mapping"
	model.beginRFActionPicker()
	if captured {
		model.rfActionQuery = fmt.Sprintf("physical key K%d press", model.rfGuideStep+1)
	}
	return model
}

func (model Model) completeRFGuidedMapping() Model {
	if model.rfGuideCandidate != nil && model.rfGuideCandidateCaptured {
		entry := *model.rfGuideCandidate
		model.rfGuideCaptures[model.rfGuideStep] = &entry
		next := -1
		for offset := 1; offset <= len(model.rfGuideCaptures); offset++ {
			index := (model.rfGuideStep + offset) % len(model.rfGuideCaptures)
			if model.rfGuideCaptures[index] == nil {
				next = index
				break
			}
		}
		if next >= 0 {
			completed := rfGuideLabels[model.rfGuideStep]
			model.rfGuideStep = next
			model.rfGuidePhase = "idle"
			model.setNotice("Button " + completed + " mapped; continue with button " + rfGuideLabels[next])
		} else {
			model.rfGuidePhase = "complete"
			model.setNotice("A/B/C/D capture complete; review stale records and test one intended signal")
		}
	} else {
		model.rfGuidePhase = "idle"
		model.setNotice("RF entry remapped; board inventory refresh requested")
	}
	model.rfGuideCandidate = nil
	model.rfGuideCandidateCaptured = false
	model.rfGuideMappingID = -1
	model.clearRFGuideArms()
	return model
}

func (model Model) handleRFGuidedKey(message tea.KeyMsg) (Model, tea.Cmd, bool) {
	key := strings.ToLower(message.String())
	if key != "delete" {
		model.rfGuideRemoveArmed = false
	}
	if key != "ctrl+backspace" {
		model.rfGuideClearArmed = false
	}
	if key != "t" {
		model.rfGuideTransmitArmed = false
	}

	switch key {
	case "esc":
		wasLearning := model.preview == nil && model.runtime.RFLearnState().Active
		model.rfGuideActive = false
		model.rfGuidePhase = ""
		model.rfGuideCandidate = nil
		model.clearRFGuideArms()
		model.setNotice("Guided RF workflow closed")
		if wasLearning {
			return model.dispatchLine("rf cancel")
		}
		return model, nil, true
	case "a", "b", "c", "d":
		if model.rfGuidePhase == "capturing" || model.rfGuidePhase == "resolving" || model.rfGuidePhase == "saving" {
			model.setNotice("Finish or cancel the current RF step before changing buttons")
			return model, nil, true
		}
		model.rfGuideStep = int(key[0] - 'a')
		model.rfGuideCandidate = nil
		model.rfGuideCandidateCaptured = false
		if model.rfGuideCaptures[model.rfGuideStep] == nil {
			model.rfGuidePhase = "idle"
			model.setNotice("Button " + strings.ToUpper(key) + " is ready to capture")
		} else {
			model.rfGuidePhase = "complete"
			model.setNotice("Button " + strings.ToUpper(key) + " is mapped and ready to review")
		}
		return model, nil, true
	case "left", "right":
		if model.rfGuidePhase == "capturing" || model.rfGuidePhase == "resolving" || model.rfGuidePhase == "saving" {
			return model, nil, true
		}
		delta := 1
		if key == "left" {
			delta = -1
		}
		model.rfGuideStep = (model.rfGuideStep + delta + len(rfGuideLabels)) % len(rfGuideLabels)
		model.rfGuidePhase = "idle"
		model.setNotice("Selected handset button " + rfGuideLabels[model.rfGuideStep])
		return model, nil, true
	case "enter":
		switch model.rfGuidePhase {
		case "identity":
			if model.rfGuideCandidate != nil {
				return model.beginRFGuidedMapping(*model.rfGuideCandidate, true), nil, true
			}
		case "idle", "interrupted", "complete":
			return model.startRFGuidedCapture()
		}
		return model, nil, true
	case "m":
		entry, ok := model.selectedRFEntry()
		if !ok {
			model.setNotice("Select a learned RF record before remapping")
			return model, nil, true
		}
		return model.beginRFGuidedMapping(entry, false), nil, true
	case "r":
		if model.rfPending {
			return model, nil, true
		}
		model.rfPending = true
		return model, model.fetchRFEntriesCommand(), true
	case "delete":
		entry, ok := model.selectedRFEntry()
		if !ok {
			model.setNotice("Select a learned RF record before removing it")
			return model, nil, true
		}
		if !model.rfGuideRemoveArmed {
			model.rfGuideRemoveArmed = true
			model.setNotice(fmt.Sprintf("Press Delete again to remove RF entry %d; Esc cancels", entry.ID))
			return model, nil, true
		}
		model.rfGuideRemoveArmed = false
		return model.dispatchLine(fmt.Sprintf("rf remove %d", entry.ID))
	case "ctrl+backspace":
		if len(model.rfStaged) == 0 {
			model.setNotice("The learned RF inventory is already empty")
			return model, nil, true
		}
		if !model.rfGuideClearArmed {
			model.rfGuideClearArmed = true
			model.setNotice(fmt.Sprintf("Press Ctrl+Backspace again to clear all %d RF records", len(model.rfStaged)))
			return model, nil, true
		}
		model.rfGuideClearArmed = false
		return model.dispatchLine("rf remove all")
	case "t":
		entry, ok := model.selectedRFEntry()
		if !ok {
			model.setNotice("Select a learned RF record before testing transmission")
			return model, nil, true
		}
		if !model.rfGuideTransmitArmed {
			model.rfGuideTransmitArmed = true
			model.setNotice(fmt.Sprintf("Press T again to transmit RF entry %d once; isolate actuators first", entry.ID))
			return model, nil, true
		}
		model.rfGuideTransmitArmed = false
		return model.dispatchLine(fmt.Sprintf(
			"rf send %d %d %d %d 1", entry.Code, entry.Bits, entry.Protocol, entry.PulseUS,
		))
	}
	return model, nil, false
}

func (model Model) rfGuidedPage() string {
	label := rfGuideLabels[model.rfGuideStep]
	phase, detail := "READY", "Press Enter to start a bounded 30-second capture for button "+label
	switch model.rfGuidePhase {
	case "capturing":
		phase, detail = "LISTENING", "Press handset button "+label+" once · Esc cancels safely"
	case "resolving":
		phase, detail = "VERIFYING", "Reading the captured record back from controller storage"
	case "identity":
		phase, detail = "CONFIRM IDENTITY", "Review every identity field · Enter confirms · Delete twice discards"
	case "mapping":
		phase, detail = "MAP ACTION", "Search the validated action catalog and press Enter to save"
	case "saving":
		phase, detail = "SAVING", "Waiting for controller acknowledgement and inventory readback"
	case "complete":
		phase, detail = "REVIEW", "Capture complete for this button · Enter recaptures · M remaps selected record"
	case "interrupted":
		phase, detail = "PAUSED", "Reconnect or resolve the current learning session, then press Enter to retry"
	}

	steps := make([]string, 0, len(rfGuideLabels))
	for index, stepLabel := range rfGuideLabels {
		state := "pending"
		if model.rfGuideCaptures[index] != nil {
			state = "mapped"
		}
		step := fmt.Sprintf(" %s  %s ", stepLabel, state)
		if index == model.rfGuideStep {
			step = selectedStyle.Render("›" + step)
		} else if state == "mapped" {
			step = buttonGoodStyle.Render("✓" + step)
		} else {
			step = buttonStyle.Render(" " + step)
		}
		steps = append(steps, step)
	}
	lines := []string{
		sectionHeader(model.width, "GUIDED RF HANDSET", "A/B/C/D · exact identity · explicit mapping · board readback"),
		lipgloss.JoinHorizontal(lipgloss.Top, intersperseStrings(steps, " ")...),
		"",
		kv("Workflow state", phase),
		kv("Current instruction", detail),
	}
	if candidate := model.rfGuideCandidate; candidate != nil {
		lines = append(lines,
			"",
			titleStyle.Render("EXACT CAPTURE IDENTITY"),
			kv("Handset button", label),
			kv("Learned slot", fmt.Sprintf("ID %d", candidate.ID)),
			kv("Code", appconfig.FormatRFCode(candidate.Code, model.rfValue.DisplayRadix)),
			kv("Waveform", fmt.Sprintf("%d bits · protocol %d · pulse %d us", candidate.Bits, candidate.Protocol, candidate.PulseUS)),
			kv("Current mapping", formatRFMappingUI(*candidate)),
		)
	}
	lines = append(lines, "", titleStyle.Render("LEARNED RECORD REVIEW"))
	if len(model.rfStaged) == 0 {
		lines = append(lines, labelStyle.Render("No learned records are stored on the controller."))
	}
	for index, entry := range model.rfStaged {
		review := "verified"
		if rfGuidedRecordNeedsReview(entry, model.rfStaged) {
			if entry.ActionKind == native.RFActionNone {
				review = "UNMAPPED"
			} else {
				review = "DUPLICATE IDENTITY"
			}
		}
		line := fmt.Sprintf(
			"ID %-2d  %-11s  %2d bit  P%-2d  %4dus  %-20s  %s",
			entry.ID, appconfig.FormatRFCode(entry.Code, model.rfValue.DisplayRadix),
			entry.Bits, entry.Protocol, entry.PulseUS, formatRFMappingUI(entry), review,
		)
		lines = append(lines, model.selectionLine(index, line))
	}
	lines = append(lines, "",
		labelStyle.Render("A/B/C/D select step · Enter capture/confirm · M remap · ↑/↓ select record · T twice test once"),
		labelStyle.Render("Delete twice removes selected · Ctrl+Backspace twice clears all · R refresh · Esc cancel/close"),
	)
	return model.scrollSelection(lines, 10)
}
