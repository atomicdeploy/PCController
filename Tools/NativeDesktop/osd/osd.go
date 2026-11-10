// Package osd presents transient desktop overlays without a board or main GUI.
package osd

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrUnsupported = errors.New("desktop OSD requires an interactive Windows session")

type Action struct {
	Label string `json:"label"`
	URI   string `json:"uri"`
}

type Request struct {
	ID           string   `json:"id,omitempty"`
	Title        string   `json:"title,omitempty"`
	Text         string   `json:"text"`
	Icon         string   `json:"icon,omitempty"`
	Severity     string   `json:"severity,omitempty"`
	DurationMS   int      `json:"duration_ms,omitempty"`
	Position     string   `json:"position,omitempty"`
	Theme        string   `json:"theme,omitempty"`
	ReduceMotion bool     `json:"reduce_motion,omitempty"`
	Actions      []Action `json:"actions,omitempty"`
}

type Status struct {
	Supported  bool      `json:"supported"`
	Active     bool      `json:"active"`
	ID         string    `json:"id,omitempty"`
	ShownAt    time.Time `json:"shown_at,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	Accepted   uint64    `json:"accepted"`
	LastAction string    `json:"last_action,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

type Presenter interface {
	Show(context.Context, Request) (Status, error)
	Dismiss(context.Context, string) (Status, error)
	Status() Status
	Close()
}

type Options struct {
	Icon func(string) (image.Image, error)
	// Activate runs only after an explicit click; the caller owns URI admission.
	Activate func(string) error
}

func New(options Options) Presenter { return newPresenter(options) }

func Normalize(r Request) (Request, error) {
	validText := func(s string, max int) bool {
		if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
			return false
		}
		for _, c := range s {
			if unicode.IsControl(c) && c != '\n' && c != '\t' {
				return false
			}
		}
		return true
	}
	r.Text = strings.TrimSpace(r.Text)
	r.Title = strings.TrimSpace(r.Title)
	if r.Text == "" || !validText(r.Text, 2048) || !validText(r.Title, 128) {
		return r, errors.New("OSD text requires 1..2048 characters; title allows at most 128, without control characters")
	}
	if len(r.ID) > 64 || !validText(r.ID, 64) || strings.ContainsAny(r.ID, "\n\t") {
		return r, errors.New("OSD id must be at most 64 characters without controls")
	}
	if r.DurationMS == 0 {
		r.DurationMS = 5000
	}
	if r.DurationMS < 500 || r.DurationMS > 120000 {
		return r, errors.New("OSD duration_ms must be 500..120000")
	}
	if r.Position == "" {
		r.Position = "bottom-center"
	}
	switch r.Position {
	case "bottom-center", "top-center", "bottom-right", "top-right":
	default:
		return r, errors.New("OSD position must be bottom-center, top-center, bottom-right or top-right")
	}
	if r.Theme == "" {
		r.Theme = "system"
	}
	switch r.Theme {
	case "system", "light", "dark":
	default:
		return r, errors.New("OSD theme must be system, light or dark")
	}
	switch r.Severity {
	case "", "info", "success", "warning", "error":
	default:
		return r, errors.New("OSD severity must be info, success, warning or error")
	}
	if r.Icon == "" {
		r.Icon = "app"
	}
	switch r.Icon {
	case "app", "info", "warning", "error", "success", "door", "hot", "temperature-cleared", "motion", "relay", "radio":
	default:
		return r, errors.New("OSD icon is not a known semantic icon")
	}
	if len(r.Actions) > 5 {
		return r, errors.New("OSD supports at most five actions")
	}
	r.Actions = append([]Action(nil), r.Actions...)
	for i, a := range r.Actions {
		if strings.TrimSpace(a.Label) == "" || !validText(a.Label, 64) || len(a.URI) > 2048 || !validText(a.URI, 2048) || strings.TrimSpace(a.URI) == "" {
			return r, fmt.Errorf("OSD action %d needs a label (1..64 characters) and URI (1..2048)", i)
		}
	}
	return r, nil
}

// Timeline is monotonic elapsed-time based, independent of dropped frames.
func Timeline(elapsed, duration time.Duration, reduced bool) (opacity float64, slide float64) {
	if elapsed < 0 || elapsed >= duration {
		return 0, 8
	}
	if reduced {
		return 1, 0
	}
	fade := 160 * time.Millisecond
	x := 1.0
	if elapsed < fade {
		x = float64(elapsed) / float64(fade)
	}
	if remaining := duration - elapsed; remaining < fade {
		x = min(x, float64(remaining)/float64(fade))
	}
	opacity = 1 - (1-x)*(1-x)*(1-x)
	return opacity, 8 * (1 - opacity)
}

// Marquee pauses at both ends and travels at 38 DIP/s, never by frame count.
func Marquee(elapsed time.Duration, overflow, scale float64, reduced bool) float64 {
	if overflow <= 0 || reduced {
		return 0
	}
	pause := 1.1
	travel := overflow / (38 * scale)
	cycle := travel*2 + pause*2
	x := float64(elapsed) / float64(time.Second)
	x -= float64(int64(x/cycle)) * cycle
	if x < pause {
		return 0
	}
	x -= pause
	if x < travel {
		return x * 38 * scale
	}
	x -= travel
	if x < pause {
		return overflow
	}
	x -= pause
	return max(0, overflow-x*38*scale)
}
