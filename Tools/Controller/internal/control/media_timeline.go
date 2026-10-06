package control

// Media-bound effects are compiled once, before playback. This is a volatile
// execution plan, not a second effect library or a wall-clock macro runner.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type MediaTimelineCue struct {
	ID         string `json:"id"`
	Reference  string `json:"reference"`
	TimeMS     uint64 `json:"time_ms"`
	DurationMS uint64 `json:"duration_ms"`
}
type MediaTimelineAction struct {
	ID     string              `json:"id"`
	TimeMS uint64              `json:"time_ms"`
	Step   appconfig.MacroStep `json:"step"`
}
type MediaTimelinePlan struct {
	ClientID      string                `json:"client_id"`
	Revision      uint64                `json:"revision"`
	Cues          []MediaTimelineCue    `json:"cues"`
	Actions       []MediaTimelineAction `json:"actions"`
	MaxLatenessMS uint32                `json:"max_lateness_ms"`
}
type MediaTimelineStatus struct {
	ClientID          string  `json:"client_id,omitempty"`
	Revision          uint64  `json:"revision"`
	Hash              string  `json:"hash,omitempty"`
	Generation        uint64  `json:"generation"`
	State             string  `json:"state"`
	StepCount         int     `json:"step_count"`
	Acknowledged      int     `json:"acknowledged"`
	ClockSequence     uint64  `json:"clock_sequence"`
	ArmedEpoch        uint64  `json:"armed_epoch"`
	RebasedSteps      int     `json:"rebased_steps"`
	ClockPositionMS   uint64  `json:"clock_position_ms"`
	LastStep          string  `json:"last_step,omitempty"`
	LastDueMS         uint64  `json:"last_due_ms"`
	LastAckLatenessMS float64 `json:"last_ack_lateness_ms"`
	MaxAckLatenessMS  float64 `json:"max_ack_lateness_ms"`
	DeviceAckUS       uint32  `json:"device_ack_us,omitempty"`
	Error             string  `json:"error,omitempty"`
}
type mediaTimelineStep struct {
	id      string
	dueMS   float64
	opcode  byte
	payload []byte
}
type mediaTimelineState struct {
	operation sync.Mutex
	mu        sync.Mutex
	status    MediaTimelineStatus
	plan      MediaTimelinePlan
	steps     []mediaTimelineStep
	cancel    context.CancelFunc
	done      chan struct{}
}
type mediaTimelineContextKey struct{}

func (runtime *Runtime) rejectMediaTimelineConflict(ctx context.Context, opcode byte) error {
	if ctx.Value(mediaTimelineContextKey{}) != nil {
		return nil
	}
	status := runtime.MediaTimeline()
	if status.State == "playing" && hostRecordableOpcode(opcode) {
		return errors.New("pause the prepared media timeline before issuing competing live output commands")
	}
	return nil
}

func (runtime *Runtime) MediaTimeline() MediaTimelineStatus {
	runtime.mediaTimeline.mu.Lock()
	defer runtime.mediaTimeline.mu.Unlock()
	return runtime.mediaTimeline.status
}

func compileMediaTimeline(plan MediaTimelinePlan, runner *MacroRunner) ([]mediaTimelineStep, error) {
	if strings.TrimSpace(plan.ClientID) == "" || plan.Revision == 0 {
		return nil, errors.New("client_id and positive timeline revision are required")
	}
	if len(plan.Cues) > 4096 || len(plan.Actions) > 65535 {
		return nil, errors.New("timeline exceeds cue/action capacity")
	}
	var steps []mediaTimelineStep
	bytes := 0
	add := func(id string, at float64, op byte, payload []byte) error {
		bytes += len(payload) + 32
		if at < 0 || at > math.MaxUint32 || len(steps) >= 65535 || bytes > 8*1024*1024 {
			return errors.New("prepared timeline exceeds time/command/RAM capacity; split the project")
		}
		steps = append(steps, mediaTimelineStep{id, at, op, append([]byte(nil), payload...)})
		return nil
	}
	ids := map[string]bool{}
	checkID := func(id string) error {
		if id == "" || len(id) > 180 || ids[id] {
			return errors.New("timeline items require unique nonempty IDs (at most 180 bytes)")
		}
		ids[id] = true
		return nil
	}
	for _, action := range plan.Actions {
		if err := checkID(action.ID); err != nil {
			return nil, err
		}
		expanded, err := expandMacroTimeline([]appconfig.MacroStep{action.Step})
		if err != nil {
			return nil, err
		}
		for i, step := range expanded {
			op, payload, err := compileMacroCommand(step)
			if err != nil {
				return nil, err
			}
			if err = add(fmt.Sprintf("%s/%d", action.ID, i), float64(action.TimeMS)+float64(step.AtUS)/1000, op, payload); err != nil {
				return nil, err
			}
		}
	}
	if len(plan.Cues) > 0 && runner == nil {
		return nil, errors.New("effect catalog is unavailable")
	}
	var strips []appconfig.StripEffect
	if runner != nil && runner.hostConfig != nil {
		strips = runner.hostConfig().StripEffects
	}
	var catalog []EffectDescriptor
	if runner != nil {
		catalog = EffectCatalog(runner.List(), strips)
	}
	for _, cue := range plan.Cues {
		if err := checkID(cue.ID); err != nil {
			return nil, err
		}
		effect, err := findEffect(catalog, cue.Reference)
		if err != nil {
			return nil, err
		}
		if effect.Kind == "sequence" {
			macro, err := runner.find(effect.ID)
			if err != nil {
				return nil, err
			}
			if macro.BoardProfileKey != "" {
				key, mode := runner.activeBoardProfile()
				if key != macro.BoardProfileKey || mode != macro.BoardProfileMode {
					return nil, fmt.Errorf("effect %s does not match the attached board profile", cue.Reference)
				}
			}
			// Use the existing compiler and persisted definition; media time, rather
			// than a second independently started macro clock, owns these offsets.
			macro.Mode = macroModeHost
			compiled, err := compileMacro(macro)
			if err != nil {
				return nil, err
			}
			for i, step := range compiled.steps {
				if err = add(fmt.Sprintf("%s/%d", cue.ID, i), float64(cue.TimeMS)+float64(step.dueUS)/1000, step.opcode, step.payload); err != nil {
					return nil, err
				}
			}
		} else if effect.Kind == "strip-stream" {
			count, fps := effect.DefaultPixels, effect.DefaultFPS
			if count < 1 || count > native.StripMaximumPixels || fps < 1 || fps > 30 {
				return nil, errors.New("strip timeline requires valid pixels and 1..30 FPS")
			}
			duration := cue.DurationMS
			if duration == 0 {
				duration = uint64(max(effect.DurationMS, 1))
			}
			if duration > math.MaxUint32 || cue.TimeMS > math.MaxUint32-duration {
				return nil, errors.New("strip cue exceeds media clock range")
			}
			frames := duration*uint64(fps)/1000 + 1
			if frames > 65535 {
				return nil, errors.New("strip cue exceeds prepared-frame capacity")
			}
			configure, err := native.StripConfigurePayload(count)
			if err != nil {
				return nil, err
			}
			if err = add(cue.ID+"/configure", float64(cue.TimeMS), native.OpAddressableLED, configure); err != nil {
				return nil, err
			}
			for n := uint64(0); n < frames; n++ {
				at := float64(n) * 1000 / float64(fps)
				if at >= float64(duration) {
					break
				}
				payloads, err := native.StripFramePayloads(renderStripProgram(effect.Program, count, time.Duration(at*float64(time.Millisecond))))
				if err != nil {
					return nil, err
				}
				for j, payload := range payloads {
					if err = add(fmt.Sprintf("%s/frame%d/%d", cue.ID, n, j), float64(cue.TimeMS)+at, native.OpAddressableLED, payload); err != nil {
						return nil, err
					}
				}
			}
			// Same bounded frame protocol; stop exactly at the authored cue end.
			payloads, _ := native.StripFramePayloads(make([]byte, count*3))
			for j, payload := range payloads {
				if err = add(fmt.Sprintf("%s/stop/%d", cue.ID, j), float64(cue.TimeMS+duration), native.OpAddressableLED, payload); err != nil {
					return nil, err
				}
			}
		} else {
			return nil, fmt.Errorf("effect %s cannot be prepared", cue.Reference)
		}
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].dueMS < steps[j].dueMS })
	return steps, nil
}

func (runtime *Runtime) PrepareMediaTimeline(plan MediaTimelinePlan) (MediaTimelineStatus, error) {
	state := &runtime.mediaTimeline
	state.operation.Lock()
	defer state.operation.Unlock()
	if runtime.emergencyStop.Load() {
		return runtime.MediaTimeline(), ErrEmergencyStopActive
	}
	if runtime.activeUseMask.Load()&activeUseStrip != 0 {
		return runtime.MediaTimeline(), errors.New("stop standalone strip streaming before preparing media playback")
	}
	if plan.MaxLatenessMS == 0 {
		plan.MaxLatenessMS = 50
	}
	if plan.MaxLatenessMS < 5 || plan.MaxLatenessMS > 250 {
		return runtime.MediaTimeline(), errors.New("max_lateness_ms must be 5..250")
	}
	state.mu.Lock()
	old := state.status
	state.mu.Unlock()
	clock := runtime.MediaPlayback()
	if clock.Connected && clock.ClientID != plan.ClientID && clock.Loaded {
		return old, errors.New("another live client owns the media timeline")
	}
	if clock.Playing {
		return old, errors.New("pause playback before preparing/replacing the hardware timeline")
	}
	steps, err := compileMediaTimeline(plan, runtime.MacroRunner())
	if err != nil {
		return old, err
	}
	if runner := runtime.MacroRunner(); runner != nil {
		if runner.State().Running || runner.RecordingState().Active {
			return old, errors.New("stop standalone effect playback/recording before preparing media playback")
		}
		for _, step := range steps {
			if step.opcode == native.OpRelaySide {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				err := requireMotionAllowed(ctx, runtime, runner.hostConfig)
				cancel()
				if err != nil {
					return old, err
				}
				break
			}
		}
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Connected {
		return old, errors.New("attach a board before preparing hardware playback")
	}
	digest, _ := json.Marshal(plan)
	// Include exact compiled bytes, not just references whose definitions may change.
	hash := sha256.New()
	hash.Write(digest)
	for _, step := range steps {
		encoded, _ := json.Marshal([]interface{}{step.id, step.dueMS, step.opcode, step.payload})
		hash.Write(encoded)
	}
	token := hex.EncodeToString(hash.Sum(nil))
	if old.Revision == plan.Revision && old.ClientID == plan.ClientID && old.Generation == snapshot.ConnectionGeneration {
		if old.Hash != token {
			return old, errors.New("a timeline revision cannot be reused with different contents")
		}
		if old.State == "faulted" {
			return old, errors.New(old.Error)
		}
		return old, nil
	}
	if old.ClientID == plan.ClientID && plan.Revision <= old.Revision {
		return old, errors.New("stale timeline revision")
	}
	state.mu.Lock()
	cancel, done := state.cancel, state.done
	state.mu.Unlock()
	if cancel != nil {
		cancel()
		if done != nil {
			<-done
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, mediaTimelineContextKey{}, true)
	done = make(chan struct{})
	state.mu.Lock()
	state.plan = plan
	state.steps = steps
	state.cancel = cancel
	state.done = done
	state.status = MediaTimelineStatus{ClientID: plan.ClientID, Revision: plan.Revision, Hash: token, Generation: snapshot.ConnectionGeneration, State: "ready", StepCount: len(steps)}
	status := state.status
	state.mu.Unlock()
	runtime.publishMediaTimeline(status)
	if len(steps) == 0 {
		close(done)
		return status, nil // Empty timelines must not leave a polling executor.
	}
	go runtime.runMediaTimeline(ctx, done, plan, steps, snapshot.ConnectionGeneration)
	return status, nil
}
func (runtime *Runtime) publishMediaTimeline(status MediaTimelineStatus) {
	runtime.PublishStructuredEvent(Event{Kind: "media.timeline", Stream: "state", Source: status.ClientID, State: status.State, Text: status.Error,
		Metadata: map[string]string{"revision": fmt.Sprint(status.Revision), "hash": status.Hash, "acknowledged": fmt.Sprint(status.Acknowledged), "step_count": fmt.Sprint(status.StepCount), "last_step": status.LastStep, "error": status.Error, "max_ack_lateness_ms": fmt.Sprint(status.MaxAckLatenessMS)}})
}
func (runtime *Runtime) stopMediaTimeline() {
	state := &runtime.mediaTimeline
	state.mu.Lock()
	cancel, done := state.cancel, state.done
	state.mu.Unlock()
	if cancel != nil {
		cancel()
		if done != nil {
			<-done
		}
	}
	state.mu.Lock()
	if state.status.State != "faulted" {
		state.status.State = "stopped"
	}
	state.mu.Unlock()
}
func (runtime *Runtime) mediaTimelineOff(generation uint64) error {
	// Pin cleanup to this board too. Never switch off a replacement board.
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), mediaTimelineContextKey{}, true), time.Second)
	defer cancel()
	var failures []error
	for _, command := range []struct {
		op      byte
		payload []byte
	}{
		{native.OpRelayAllOff, nil}, {native.OpPWMAllOff, nil},
		{native.OpAddressableLED, []byte{native.AddressableLEDFill, 0, 0, 0, 0xFF}},
		{native.OpBuzzer, native.BuzzerPayload(0, 1)},
	} {
		if _, err := runtime.requestAtGeneration(ctx, generation, command.op, command.payload, native.OpACK); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Restore latched values after pause/seek without replaying historical RF,
// buzzer, menu, or display actions. The full strip frame is a single state.
func mediaTimelineRestore(steps []mediaTimelineStep, index int) []mediaTimelineStep {
	last := map[string]int{}
	stripStart := -1
	for i, step := range steps[:index] {
		switch step.opcode {
		case native.OpRelaySet, native.OpRelaySide, native.OpPWMSet:
			if len(step.payload) > 0 {
				last[fmt.Sprintf("%d/%d", step.opcode, step.payload[0])] = i
			}
		case native.OpRelayAllOff:
			for key, j := range last {
				if steps[j].opcode == native.OpRelaySet || steps[j].opcode == native.OpRelaySide {
					delete(last, key)
				}
			}
		case native.OpPWMAllOff:
			for key, j := range last {
				if steps[j].opcode == native.OpPWMSet {
					delete(last, key)
				}
			}
		case native.OpStatusRGB:
			last["rgb"] = i
		case native.OpAddressableLED:
			if len(step.payload) >= 2 && step.payload[0] == 0xFD && step.payload[1] == 0 {
				stripStart = i
			}
			if len(step.payload) > 0 && step.payload[0] == native.AddressableLEDFill {
				stripStart = i
			}
		}
	}
	var restored []mediaTimelineStep
	for _, i := range last {
		restored = append(restored, steps[i])
	}
	sort.Slice(restored, func(i, j int) bool { return restored[i].dueMS < restored[j].dueMS })
	if stripStart >= 0 {
		for _, step := range steps[stripStart:index] {
			if step.opcode == native.OpAddressableLED {
				restored = append(restored, step)
			}
		}
	}
	return restored
}
func (runtime *Runtime) runMediaTimeline(ctx context.Context, done chan struct{}, plan MediaTimelinePlan, steps []mediaTimelineStep, generation uint64) {
	defer close(done)
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	index := 0
	epoch := ^uint64(0)
	wasPlaying := false
	owned := false
	defer func() {
		if owned {
			if err := runtime.mediaTimelineOff(generation); err != nil {
				runtime.mediaTimeline.mu.Lock()
				runtime.mediaTimeline.status.State = "faulted"
				runtime.mediaTimeline.status.Error += "; output-off cleanup not acknowledged: " + err.Error()
				status := runtime.mediaTimeline.status
				runtime.mediaTimeline.mu.Unlock()
				runtime.publishMediaTimeline(status)
			}
		}
	}()
	fault := func(message string) {
		runtime.mediaTimeline.mu.Lock()
		runtime.mediaTimeline.status.State = "faulted"
		runtime.mediaTimeline.status.Error = message
		status := runtime.mediaTimeline.status
		runtime.mediaTimeline.mu.Unlock()
		runtime.publishMediaTimeline(status)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		clock, anchor := runtime.mediaTimelineClock()
		if clock.ClientID != plan.ClientID || clock.PlanRevision != plan.Revision {
			if wasPlaying {
				fault("prepared timeline/clock ownership changed during playback")
				return
			}
			continue
		}
		if anchor.IsZero() || time.Since(anchor) > 250*time.Millisecond {
			if clock.Playing || wasPlaying {
				fault("video clock feedback expired; seek before the pending cue and reprepare")
				return
			}
			continue
		}
		if runtime.emergencyStop.Load() {
			fault("E-STOP interrupted prepared playback")
			return
		}
		runtime.mu.RLock()
		connected, currentGeneration := runtime.session != nil, runtime.generation
		runtime.mu.RUnlock()
		if !connected || currentGeneration != generation {
			fault("prepared board session was disconnected/replaced; reprepare")
			return
		}
		position := float64(clock.PositionMS)
		if clock.Playing {
			position += time.Since(anchor).Seconds() * 1000 * clock.Rate
		}
		if epoch != clock.Epoch {
			if clock.Playing {
				fault("media epoch changed without a paused arming acknowledgement")
				return
			}
			index = sort.Search(len(steps), func(i int) bool { return steps[i].dueMS >= position })
			runtime.mediaTimeline.mu.Lock()
			runtime.mediaTimeline.status.ArmedEpoch = clock.Epoch
			runtime.mediaTimeline.status.RebasedSteps = index
			runtime.mediaTimeline.mu.Unlock()
			epoch = clock.Epoch
		}
		if wasPlaying && !clock.Playing && owned {
			if err := runtime.mediaTimelineOff(generation); err != nil {
				fault("pause output-off cleanup not acknowledged: " + err.Error())
				return
			}
			owned = false
		}
		resuming := !wasPlaying && clock.Playing
		if resuming {
			if runner := runtime.MacroRunner(); runner != nil && (runner.State().Running || runner.RecordingState().Active) {
				fault("standalone effect playback/recording conflicts with prepared media playback")
				return
			}
		}
		wasPlaying = clock.Playing
		runtime.mediaTimeline.mu.Lock()
		stateName := "paused"
		if clock.Playing {
			stateName = "playing"
		}
		runtime.mediaTimeline.status.State = stateName
		runtime.mediaTimeline.status.ClockSequence = clock.Sequence
		runtime.mediaTimeline.status.ClockPositionMS = uint64(position)
		runtime.mediaTimeline.mu.Unlock()
		if !clock.Playing {
			continue
		}
		if resuming && index > 0 {
			for _, step := range mediaTimelineRestore(steps, index) {
				owned = true // A lost ACK can still mean the hardware applied it.
				requestCtx, cancel := context.WithTimeout(ctx, time.Duration(plan.MaxLatenessMS)*time.Millisecond)
				_, err := runtime.requestAtGeneration(requestCtx, generation, step.opcode, step.payload, native.OpACK)
				cancel()
				if err != nil {
					fault(fmt.Sprintf("resume state %s not acknowledged: %v", step.id, err))
					return
				}
			}
			position = float64(clock.PositionMS) + time.Since(anchor).Seconds()*1000*clock.Rate
		}
		for index < len(steps) && steps[index].dueMS <= position {
			step := steps[index]
			runtime.mediaTimeline.mu.Lock()
			runtime.mediaTimeline.status.LastStep, runtime.mediaTimeline.status.LastDueMS = step.id, uint64(step.dueMS)
			runtime.mediaTimeline.mu.Unlock()
			if position-step.dueMS > float64(plan.MaxLatenessMS) {
				fault(fmt.Sprintf("cue %s missed its deadline by %.1f ms; not executed", step.id, position-step.dueMS))
				return
			}
			requestCtx, cancel := context.WithTimeout(ctx, time.Duration(plan.MaxLatenessMS)*time.Millisecond)
			owned = true
			frame, err := runtime.requestAtGeneration(requestCtx, generation, step.opcode, step.payload, native.OpACK)
			cancel()
			actual := float64(clock.PositionMS) + time.Since(anchor).Seconds()*1000*clock.Rate
			lateness := actual - step.dueMS
			runtime.mediaTimeline.mu.Lock()
			runtime.mediaTimeline.status.LastStep = step.id
			runtime.mediaTimeline.status.LastDueMS = uint64(step.dueMS)
			runtime.mediaTimeline.status.LastAckLatenessMS = lateness
			runtime.mediaTimeline.status.MaxAckLatenessMS = max(runtime.mediaTimeline.status.MaxAckLatenessMS, lateness)
			if deviceUS, ok := native.ResponseDeviceMicros(frame); ok {
				runtime.mediaTimeline.status.DeviceAckUS = deviceUS
			}
			if err == nil {
				runtime.mediaTimeline.status.Acknowledged++
				owned = true
			}
			status := runtime.mediaTimeline.status
			runtime.mediaTimeline.mu.Unlock()
			if err != nil {
				fault(fmt.Sprintf("cue %s not acknowledged: %v", step.id, err))
				return
			}
			runtime.publishCommandEvidence(acknowledgedCommandEvidence(ctx, step.opcode, step.payload, frame))
			runtime.publishMediaTimeline(status)
			if lateness > float64(plan.MaxLatenessMS) {
				fault(fmt.Sprintf("cue %s ACK was %.1f ms late; playback interrupted", step.id, lateness))
				return
			}
			index++
			position = actual
		}
	}
}
