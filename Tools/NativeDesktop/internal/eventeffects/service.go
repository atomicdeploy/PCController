// Package eventeffects owns host-neutral reactions to application-core events.
//
// Window, tray, hotkey, and terminal hosts can subscribe independently while
// sharing one policy for automation scripts and audible cues.
package eventeffects

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/sfx"
)

const (
	audioQueueSize = 64
	beepInterval   = 220 * time.Millisecond
)

// Cue identifies one semantic audible cue without tying event policy to a
// platform sound implementation.
type Cue = sfx.Cue

const (
	CueOn      = sfx.CueOn
	CueOff     = sfx.CueOff
	CueMessage = sfx.CueMessage
	CueError   = sfx.CueError
)

// Automation is the event-driven portion of automation.Runner. Keeping this
// interface small makes the shared policy independently testable.
type Automation interface {
	ApplyConfig(config.AutomationConfig)
	Handle(model.Event)
	Close()
}

// PlayCue starts one audible cue. Production resolves the same bundled or
// imported PCM WAV used by the GUI and Web surfaces; tests can provide a
// deterministic recorder.
type PlayCue func(Cue) error

// LogFunc receives non-fatal effect errors and queue-pressure warnings.
type LogFunc func(message string)

// Options contains the dependencies of Service.
type Options struct {
	Config     config.Config
	Events     <-chan model.Event
	Automation Automation
	PlayCue    PlayCue
	Log        LogFunc
}

type audioJob struct {
	cue   Cue
	count int
}

// Service consumes an application-core subscription and owns the automation
// and sound effects associated with that subscription.
type Service struct {
	automation Automation
	playCue    PlayCue
	sounds     *sfx.Manager
	log        LogFunc
	events     <-chan model.Event

	cfgMu sync.RWMutex
	cfg   config.Config

	ctx       context.Context
	cancel    context.CancelFunc
	audio     chan audioJob
	eventDone chan struct{}
	audioDone chan struct{}

	closeOnce sync.Once
}

// New starts a host-neutral event-effects service.
func New(options Options) (*Service, error) {
	return newService(options, beepInterval)
}

func newService(options Options, interval time.Duration) (*Service, error) {
	switch {
	case options.Events == nil:
		return nil, errors.New("eventeffects: nil event stream")
	case options.Automation == nil:
		return nil, errors.New("eventeffects: nil automation runner")
	case options.PlayCue == nil:
		return nil, errors.New("eventeffects: nil cue player")
	case interval < 0:
		return nil, errors.New("eventeffects: negative beep interval")
	}

	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		automation: options.Automation,
		playCue:    options.PlayCue,
		log:        options.Log,
		events:     options.Events,
		cfg:        options.Config,
		ctx:        ctx,
		cancel:     cancel,
		audio:      make(chan audioJob, audioQueueSize),
		eventDone:  make(chan struct{}),
		audioDone:  make(chan struct{}),
	}
	go service.runEvents()
	go service.runAudio(interval)
	return service, nil
}

// PrepareConfig resolves host-owned sound assets without publishing runtime
// policy. Callers should persist and expose the returned repaired config.
func (s *Service) PrepareConfig(
	cfg config.Config,
) (config.Config, error) {
	if s == nil || s.sounds == nil {
		return cfg, nil
	}
	effective, warning := s.sounds.PreflightNotifications(cfg.Notifications)
	cfg.Notifications = effective
	if warning != nil {
		s.logf(
			"selected custom sound is unavailable; repaired to bundled sounds: %v",
			warning,
		)
	}
	return cfg, nil
}

// ApplyConfig immediately replaces the notification and automation policy used
// for subsequent events. It is safe to call from the command dispatcher.
func (s *Service) ApplyConfig(cfg config.Config) {
	if s == nil {
		return
	}
	select {
	case <-s.ctx.Done():
		return
	default:
	}

	cfg, _ = s.PrepareConfig(cfg)
	if s.sounds != nil {
		if err := s.sounds.ApplyConfig(cfg.Notifications); err != nil {
			s.logf("apply audible cue configuration: %v", err)
			return
		}
	}
	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgMu.Unlock()
	s.automation.ApplyConfig(cfg.Automation)
}

// Play enqueues an explicit cue independently of toast delivery and the
// incidental notification-sound preference. It is used by diagnostics,
// command surfaces, and accessibility checks.
func (s *Service) Play(cue Cue) error {
	return s.PlayCount(cue, 1)
}

// PlayCount queues a bounded sequence for an explicit command surface.
func (s *Service) PlayCount(cue Cue, count int) error {
	if s == nil {
		return errors.New("eventeffects: service is unavailable")
	}
	switch cue {
	case CueOn, CueOff, CueMessage, CueError:
	default:
		return fmt.Errorf("eventeffects: unknown cue %q", cue)
	}
	if count < 1 || count > 10 {
		return errors.New("eventeffects: cue count must be between 1 and 10")
	}
	select {
	case <-s.ctx.Done():
		return errors.New("eventeffects: service is closed")
	case s.audio <- audioJob{cue: cue, count: count}:
		return nil
	default:
		return errors.New("eventeffects: audible cue queue is full")
	}
}

// ResolveAudio returns the exact configured asset for browser playback.
func (s *Service) ResolveAudio(cue string) (sfx.Asset, error) {
	if s == nil || s.sounds == nil {
		return sfx.Asset{}, errors.New("eventeffects: sound assets are unavailable")
	}
	return s.sounds.Resolve(sfx.Cue(cue))
}

// ImportAudio stores one validated, content-addressed custom sound.
func (s *Service) ImportAudio(name string, wave []byte) (sfx.ImportedAsset, error) {
	if s == nil || s.sounds == nil {
		return sfx.ImportedAsset{}, errors.New("eventeffects: sound assets are unavailable")
	}
	return s.sounds.Import(name, wave)
}

// ImportAudioFile validates and stores one local audio file for TUI/IPC callers.
func (s *Service) ImportAudioFile(path string) (sfx.ImportedAsset, error) {
	if s == nil || s.sounds == nil {
		return sfx.ImportedAsset{}, errors.New("eventeffects: sound assets are unavailable")
	}
	return s.sounds.ImportFile(path)
}

// Close stops event intake and audio playback, then joins the automation
// runner. It is concurrent and idempotent.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.eventDone
		<-s.audioDone
		s.automation.Close()
	})
}

func (s *Service) runEvents() {
	defer close(s.eventDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-s.events:
			if !ok {
				return
			}
			s.automation.Handle(event)
			s.handleAudio(event)
		}
	}
}

func (s *Service) handleAudio(event model.Event) {
	cfg := s.config()
	switch event.Kind {
	case model.EventSnapshot:
		changed := event.PreviousState != event.Snapshot.State &&
			event.Snapshot.State != model.StateUnknown
		if !changed ||
			(event.InitialSync && !cfg.Notifications.InitialSync) ||
			!cfg.Notifications.Sound {
			return
		}
		cue := CueOff
		if event.Snapshot.State == model.StateOn {
			cue = CueOn
		}
		s.enqueue(audioJob{cue: cue, count: 1})
	case model.EventMessage:
		if cfg.Notifications.Sound {
			s.enqueue(audioJob{cue: CueMessage, count: 1})
		}
	case model.EventBeep:
		count := event.BeepCount
		if count < 1 {
			count = 1
		}
		if count > 10 {
			count = 10
		}
		// A beep event is an explicit audible command from the device. It remains
		// audible even when incidental notification sounds are disabled, which
		// matches the GUI shell.
		s.enqueue(audioJob{cue: CueMessage, count: count})
	}
}

func (s *Service) config() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Service) enqueue(job audioJob) {
	select {
	case <-s.ctx.Done():
	case s.audio <- job:
	default:
		s.logf("audible cue queue is full; skipped %s cue", job.cue)
	}
}

func (s *Service) runAudio(interval time.Duration) {
	defer close(s.audioDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		select {
		case <-s.ctx.Done():
			return
		case job := <-s.audio:
			for index := 0; index < job.count; index++ {
				if err := s.playCue(job.cue); err != nil {
					s.logf("play %s cue: %v", job.cue, err)
				}
				if index+1 >= job.count || interval == 0 {
					continue
				}
				timer := time.NewTimer(interval)
				select {
				case <-s.ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return
				case <-timer.C:
				}
			}
		}
	}
}

func (s *Service) logf(format string, args ...any) {
	if s.log != nil {
		s.log(fmt.Sprintf(format, args...))
	}
}
