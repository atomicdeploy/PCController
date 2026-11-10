//go:build windows

package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"pccontroller.local/desktop/internal/dialogs"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
)

var controllerMotionCaptureProc = windows.NewLazySystemDLL("user32.dll").NewProc("GetCapture")

func controllerMotionCapture() win.HWND {
	handle, _, _ := controllerMotionCaptureProc.Call()
	return win.HWND(handle)
}

// Seat holds are separate from the retained segment/chrome animation renderer.
func (s *Shell) openControllerMotion() {
	s.openControllerMotionObserved(nil)
}

func (s *Shell) openControllerMotionObserved(observe func(*walk.Dialog)) {
	var dialog *walk.Dialog
	var closeButton *walk.PushButton
	var status *walk.Label
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	closed, held, renewing, started, stopping := false, false, false, false, false
	var epoch uint64
	var lease presentation.MotionHoldLease
	var heldButton *walk.PushButton
	keyboard := false
	release := func(old presentation.MotionHoldLease) {
		if old.ID == "" {
			return
		}
		go func() {
			request, done := context.WithTimeout(context.Background(), 2*time.Second)
			defer done()
			_, err := s.controller.MotionHold(request, "release", old, "")
			if err != nil {
				dialog.Synchronize(func() {
					if !closed {
						status.SetText("Stop is unconfirmed: " + err.Error())
					}
				})
			}
		}()
	}
	stop := func() {
		epoch++
		held = false
		started = false
		heldButton = nil
		old := lease
		lease = presentation.MotionHoldLease{}
		release(old)
	}
	defer stop()
	start := func(button *walk.PushButton, seat presentation.Seat, direction string, key bool) {
		if closed || held || stopping || s.controller.MotionHold == nil || !s.controllerState.Connected || s.controllerState.Busy || seat.Locked {
			return
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			status.SetText(err.Error())
			return
		}
		epoch++
		thisEpoch := epoch
		held = true
		heldButton = button
		keyboard = key
		status.SetText("")
		requestLease := presentation.MotionHoldLease{ID: hex.EncodeToString(nonce[:]), ExpiresAt: time.Now().Add(700 * time.Millisecond), Side: seat.Side, Generation: s.controllerState.Generation}
		lease = requestLease
		started = false
		go func() {
			request, done := context.WithTimeout(ctx, 2*time.Second)
			defer done()
			next, err := s.controller.MotionHold(request, "start", requestLease, direction)
			dialog.Synchronize(func() {
				if closed || !held || thisEpoch != epoch {
					release(next)
					return
				}
				if err != nil {
					stop()
					status.SetText(err.Error())
					return
				}
				lease = next
				started = true
			})
		}()
	}
	type motionRow struct {
		seat           presentation.Seat
		state          *walk.Label
		up, down, stop *walk.PushButton
	}
	var rows []*motionRow
	children := []d.Widget{d.Label{Text: "Seat & motion", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 16, Bold: true}}}
	for _, seat := range s.controllerState.Seats {
		row := &motionRow{seat: seat}
		rows = append(rows, row)
		children = append(children, d.GroupBox{Title: seat.Name, Layout: d.VBox{Spacing: 12}, Children: []d.Widget{
			d.Label{AssignTo: &row.state, Text: seat.State, MinSize: d.Size{Height: 28}},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 10}, Children: []d.Widget{
				d.PushButton{AssignTo: &row.up, Text: "Hold Up", Enabled: s.controller.MotionHold != nil && s.controllerState.Connected && !s.controllerState.Busy && !seat.Locked, StretchFactor: 1, MinSize: d.Size{Height: 40}},
				d.PushButton{AssignTo: &row.stop, Text: "Stop", Enabled: s.controller.InvokeAction != nil && s.controllerState.Connected && !s.controllerState.Busy && seat.StopAction != "", StretchFactor: 1, MinSize: d.Size{Height: 40}, OnClicked: func() {
					if stopping {
						return
					}
					hadLease := lease.ID != ""
					stop()
					if hadLease {
						return
					}
					if s.controller.InvokeAction != nil && row.seat.StopAction != "" {
						stopping = true
						owner := s.controllerState.ControlOwner
						go func() {
							request, done := context.WithTimeout(ctx, 2*time.Second)
							defer done()
							err := s.controller.InvokeAction(request, owner, row.seat.StopAction)
							dialog.Synchronize(func() {
								stopping = false
								if !closed {
									if err != nil {
										status.SetText(err.Error())
									}
								}
							})
						}()
					}
				}},
				d.PushButton{AssignTo: &row.down, Text: "Hold Down", Enabled: s.controller.MotionHold != nil && s.controllerState.Connected && !s.controllerState.Busy && !seat.Locked, StretchFactor: 1, MinSize: d.Size{Height: 40}},
			}},
		}})
	}
	if len(rows) == 0 {
		children = append(children, d.Label{Text: "Choose a cinema-seat board profile to enable motion controls."})
	}
	children = append(children, d.VSpacer{}, d.Label{AssignTo: &status, MinSize: d.Size{Height: 30}}, d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{d.HSpacer{}, d.PushButton{AssignTo: &closeButton, Text: "Close", OnClicked: func() { dialog.Cancel() }}}})
	spec := d.Dialog{AssignTo: &dialog, Title: "Seat & motion — PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 480, Height: 380}, Size: d.Size{Width: 600, Height: 460}, Layout: d.VBox{Margins: d.Margins{Left: 20, Right: 20, Top: 20, Bottom: 18}, Spacing: 16}, CancelButton: &closeButton, Children: children}
	if err := spec.Create(s.mw); err != nil {
		s.showError("Seat & motion", err)
		return
	}
	defer dialog.Dispose()
	geometry, err := winfx.AttachResponsiveDialogGeometry(uintptr(dialog.Handle()))
	if err != nil {
		s.showError("Seat & motion", err)
		return
	}
	defer geometry.Dispose()
	theme := dialogs.NewThemeSession(dialog)
	defer theme.Dispose()
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	_ = theme.WatchSystemChanges(nil)
	_ = dialog.SetIcon(s.appIcon)
	for _, row := range rows {
		for _, item := range []struct {
			button    *walk.PushButton
			direction string
		}{{row.up, "up"}, {row.down, "down"}} {
			button, direction := item.button, item.direction
			button.MouseDown().Attach(func(_, _ int, b walk.MouseButton) {
				if b == walk.LeftButton {
					start(button, row.seat, direction, false)
				}
			})
			button.MouseUp().Attach(func(_, _ int, b walk.MouseButton) {
				if b == walk.LeftButton {
					stop()
				}
			})
			button.KeyDown().Attach(func(key walk.Key) {
				if key == walk.KeySpace {
					start(button, row.seat, direction, true)
				}
			})
			button.KeyUp().Attach(func(key walk.Key) {
				if key == walk.KeySpace {
					stop()
				}
			})
			button.FocusedChanged().Attach(func() {
				if heldButton == button && !button.Focused() {
					stop()
				}
			})
		}
	}
	dialog.Deactivating().Attach(stop)
	dialog.Closing().Attach(func(*bool, walk.CloseReason) { stop(); closed = true; cancel() })
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dialog.Synchronize(func() {
					if closed {
						return
					}
					for _, row := range rows {
						enabled := false
						for _, seat := range s.controllerState.Seats {
							if seat.Key == row.seat.Key {
								row.seat = seat
								row.state.SetText(seat.State)
								enabled = s.controller.MotionHold != nil && s.controllerState.Connected && !s.controllerState.Busy && !stopping && !seat.Locked
								break
							}
						}
						row.up.SetEnabled(enabled)
						row.down.SetEnabled(enabled)
						row.stop.SetEnabled(held || s.controller.InvokeAction != nil && s.controllerState.Connected && !s.controllerState.Busy && !stopping && row.seat.StopAction != "")
					}
					if held && (s.controllerState.Busy || !s.controllerState.Connected || lease.ID != "" && lease.Generation != s.controllerState.Generation || heldButton != nil && (!heldButton.Enabled() || keyboard && !heldButton.Focused() || !keyboard && controllerMotionCapture() != heldButton.Handle())) {
						stop()
						return
					}
					if !held || !started || lease.ID == "" || renewing {
						return
					}
					renewing = true
					old, thisEpoch := lease, epoch
					go func() {
						request, done := context.WithTimeout(ctx, 500*time.Millisecond)
						defer done()
						next, err := s.controller.MotionHold(request, "renew", old, "")
						dialog.Synchronize(func() {
							renewing = false
							if closed || thisEpoch != epoch {
								return
							}
							if err != nil {
								stop()
								status.SetText(err.Error())
							} else {
								lease = next
							}
						})
					}()
				})
			}
		}
	}()
	if observe != nil {
		observe(dialog)
	}
	dialog.Run()
}
