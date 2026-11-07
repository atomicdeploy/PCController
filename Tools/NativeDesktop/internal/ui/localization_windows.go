//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"pccontroller.local/desktop/internal/i18n"
	"pccontroller.local/desktop/internal/winfx"
)

const defaultMainFontFamily = "Segoe UI"

type mainFontSet struct {
	title   *walk.Font
	state   *walk.Font
	body    *walk.Font
	button  *walk.Font
	caption *walk.Font
}

func (fonts mainFontSet) dispose() {
	for _, font := range []*walk.Font{
		fonts.title,
		fonts.state,
		fonts.body,
		fonts.button,
		fonts.caption,
	} {
		if font != nil {
			font.Dispose()
		}
	}
}

func newMainFontSet(family string) (mainFontSet, error) {
	if family == "" {
		family = defaultMainFontFamily
	}
	var fonts mainFontSet
	specs := []struct {
		target **walk.Font
		size   int
		style  walk.FontStyle
	}{
		{&fonts.title, 12, walk.FontBold},
		{&fonts.state, 22, walk.FontBold},
		{&fonts.body, 10, 0},
		{&fonts.button, 11, walk.FontBold},
		{&fonts.caption, 9, 0},
	}
	for _, spec := range specs {
		face := family
		if family == winfx.PersianFontFamily(false) {
			face = winfx.PersianFontFamily(spec.style&walk.FontBold != 0)
		}
		font, err := walk.NewFont(face, spec.size, spec.style)
		if err != nil {
			fonts.dispose()
			return mainFontSet{}, fmt.Errorf(
				"create %s %dpt main font: %w",
				family,
				spec.size,
				err,
			)
		}
		*spec.target = font
	}
	return fonts, nil
}

func (s *Shell) initializeLocalization() {
	if s == nil {
		return
	}
	localizer := i18n.New(i18n.Locale(s.cfg.UI.Locale))
	s.localizer = localizer
	s.fontFamily = defaultMainFontFamily

	if err := winfx.EnsurePersianFont(); err != nil {
		s.logf(
			"register Vazirmatn privately; falling back to Segoe UI: %v",
			err,
		)
		return
	}
	s.persianFontAvailable = true
	if localizer.IsRTL() {
		s.fontFamily = winfx.PersianFontFamily(false)
	}
}

func (s *Shell) localizerSnapshot() i18n.Localizer {
	if s == nil {
		return i18n.New(i18n.English)
	}
	s.localeMu.RLock()
	localizer := s.localizer
	s.localeMu.RUnlock()
	return localizer
}

func (s *Shell) tr(id i18n.MessageID) string {
	if s.controller != nil && id == i18n.MsgAppName {
		return s.controller.Title
	}
	return s.localizerSnapshot().Text(id)
}

func (s *Shell) nativeText(text string) string { return s.localizerSnapshot().NativeText(text) }

func (s *Shell) trHeadline(id i18n.MessageID) string {
	text := s.tr(id)
	if s.isRTL() {
		return text
	}
	return strings.ToUpper(text)
}

func (s *Shell) isRTL() bool {
	return s.localizerSnapshot().IsRTL()
}

func (s *Shell) rtlTextFlag() walk.DrawTextFormat {
	if s.isRTL() {
		return walk.TextRTLReading
	}
	return 0
}

func (s *Shell) preferredFontFamily() string {
	if s == nil {
		return defaultMainFontFamily
	}
	s.localeMu.RLock()
	family := s.fontFamily
	s.localeMu.RUnlock()
	if family == "" {
		return defaultMainFontFamily
	}
	return family
}

func (s *Shell) replaceMainFonts(family string) error {
	fonts, err := newMainFontSet(family)
	if err != nil {
		return err
	}
	previous := mainFontSet{
		title:   s.titleFont,
		state:   s.stateFont,
		body:    s.bodyFont,
		button:  s.buttonFont,
		caption: s.captionFont,
	}
	s.disposeSurfaceFonts()
	s.titleFont = fonts.title
	s.stateFont = fonts.state
	s.bodyFont = fonts.body
	s.buttonFont = fonts.button
	s.captionFont = fonts.caption
	previous.dispose()
	return nil
}

// applyLocaleUI updates text direction and the complete main-window font set
// atomically on the Walk UI thread. If Yekan cannot be instantiated, the
// localized Persian copy remains active with Windows' Segoe UI fallback.
func (s *Shell) applyLocaleUI(preference string) {
	if s == nil {
		return
	}
	next := i18n.New(i18n.Locale(preference))
	family := defaultMainFontFamily
	if next.IsRTL() && s.persianFontAvailable {
		family = winfx.PersianFontFamily(false)
	}

	current := s.localizerSnapshot()
	currentFamily := s.preferredFontFamily()
	if current.Locale() == next.Locale() && currentFamily == family {
		return
	}
	if err := s.replaceMainFonts(family); err != nil {
		s.logf("apply %s main font; falling back to Segoe UI: %v", family, err)
		family = defaultMainFontFamily
		if family != currentFamily {
			if fallbackErr := s.replaceMainFonts(family); fallbackErr != nil {
				s.logf("apply Segoe UI main font: %v", fallbackErr)
				// Keep the existing usable font set. A font failure must not
				// prevent the requested language and RTL layout from applying.
				family = currentFamily
			}
		}
	}
	s.localeMu.Lock()
	s.localizer = next
	s.fontFamily = family
	s.localeMu.Unlock()
	if s.mw != nil {
		if err := s.mw.SetTitle(next.Text(i18n.MsgAppName)); err != nil {
			s.logf("update localized main-window title: %v", err)
		}
	}
}
