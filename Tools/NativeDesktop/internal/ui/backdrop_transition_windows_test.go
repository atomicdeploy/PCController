//go:build windows

package ui

import (
	"errors"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/winfx"
)

func TestImmediateBackdropCommitRequired(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		next     string
		want     bool
	}{
		{"transparent to auto", winfx.BackdropTransparent, winfx.BackdropAuto, true},
		{"transparent to blur", winfx.BackdropTransparent, winfx.BackdropBlur, true},
		{
			"transparent to acrylic",
			winfx.BackdropTransparent,
			winfx.BackdropAcrylic,
			true,
		},
		{"transparent to mica", winfx.BackdropTransparent, winfx.BackdropMica, true},
		{
			"transparent to tabbed",
			winfx.BackdropTransparent,
			winfx.BackdropTabbed,
			true,
		},
		{
			"transparent unchanged",
			winfx.BackdropTransparent,
			winfx.BackdropTransparent,
			false,
		},
		{"transparent to solid", winfx.BackdropTransparent, winfx.BackdropSolid, false},
		{"solid to acrylic", winfx.BackdropSolid, winfx.BackdropAcrylic, false},
		{"blur to acrylic", winfx.BackdropBlur, winfx.BackdropAcrylic, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if got := immediateBackdropCommitRequired(
				test.previous,
				test.next,
			); got != test.want {
				t.Errorf("immediate commit = %t, want %t", got, test.want)
			}
		})
	}
}

func TestCommitBackdropTransitionOrdersEveryCompositionStage(t *testing.T) {
	frameErr := errors.New("frame warning")
	flushErr := errors.New("flush warning")
	var stages []string
	stage := func(name string, err error) func() error {
		return func() error {
			stages = append(stages, name)
			return err
		}
	}

	err := commitBackdropTransition(backdropCommitOps{
		recalculateFrame: stage("frame", frameErr),
		presentSurface:   stage("present", nil),
		redrawFrame:      stage("redraw", nil),
		flushComposition: stage("flush", flushErr),
	})
	if got, want := strings.Join(stages, ","), "frame,present,redraw,flush"; got != want {
		t.Fatalf("composition stages = %q, want %q", got, want)
	}
	if !errors.Is(err, frameErr) || !errors.Is(err, flushErr) {
		t.Fatalf("composition errors = %v", err)
	}
}
