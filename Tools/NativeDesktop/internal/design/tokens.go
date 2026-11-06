// Package design exposes the canonical visual tokens shared by native and web
// PCControllerDesktop surfaces.
package design

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

const (
	AccentCyan       = "#55D3FF"
	AccentCyanStrong = "#24C8FF"
	AccentCyanSoft   = "#A9ECFF"
	AccentWarm       = "#FFD465"
	AccentWarmStrong = "#FFB83E"
	AccentSuccess    = "#72E3A6"
	AccentDanger     = "#FF7890"
	DarkAccent       = "#22BEF2"
	DarkAccentHover  = "#4AD3FF"
	DarkWarm         = "#FFD35B"
	DarkSuccess      = "#4BDA97"
	DarkDanger       = "#FF677D"
	LightAccent      = "#0089BE"
	LightAccentHover = "#00A3DC"
	LightWarm        = "#CC8400"
	LightSuccess     = "#108B54"
	LightDanger      = "#C72E47"
)

// JSON is the canonical UTF-8 token document consumed by the frontend build.
//
//go:embed tokens.json
var JSON []byte

type Tokens struct {
	Colors   Colors   `json:"colors"`
	Surfaces Surfaces `json:"surfaces"`
	Text     Text     `json:"text"`
	Strokes  Strokes  `json:"strokes"`
	Radii    Radii    `json:"radii"`
	Spacing  Spacing  `json:"spacing"`
	Motion   Motion   `json:"motion"`
}

type Colors struct {
	Accent AccentPalette `json:"accent"`
	Dark   ThemePalette  `json:"dark"`
	Light  ThemePalette  `json:"light"`
}

type AccentPalette struct {
	Cyan       string `json:"cyan"`
	CyanStrong string `json:"cyan_strong"`
	CyanSoft   string `json:"cyan_soft"`
	Warm       string `json:"warm"`
	WarmStrong string `json:"warm_strong"`
	Success    string `json:"success"`
	Danger     string `json:"danger"`
}

type ThemePalette struct {
	Accent          string `json:"accent"`
	AccentHover     string `json:"accent_hover"`
	Warm            string `json:"warm"`
	Success         string `json:"success"`
	Danger          string `json:"danger"`
	Canvas          string `json:"canvas"`
	CanvasAlt       string `json:"canvas_alt"`
	Surface         string `json:"surface"`
	SurfaceHigh     string `json:"surface_high"`
	SurfaceOverlay  string `json:"surface_overlay"`
	Text            string `json:"text"`
	TextMuted       string `json:"text_muted"`
	TextSubtle      string `json:"text_subtle"`
	Stroke          string `json:"stroke"`
	StrokeHighlight string `json:"stroke_highlight"`
	Shadow          string `json:"shadow"`
}

type Surfaces struct {
	GlassOpacityDark  float64 `json:"glass_opacity_dark"`
	GlassOpacityLight float64 `json:"glass_opacity_light"`
	GlassBlurPX       int     `json:"glass_blur_px"`
	GlassSaturation   float64 `json:"glass_saturation"`
	CardElevation     string  `json:"card_elevation"`
	ControlElevation  string  `json:"control_elevation"`
}

type Text struct {
	FontFamily    string  `json:"font_family"`
	FontMono      string  `json:"font_mono"`
	SizeCaptionPX int     `json:"size_caption_px"`
	SizeBodyPX    int     `json:"size_body_px"`
	SizeTitlePX   int     `json:"size_title_px"`
	SizeDisplayPX int     `json:"size_display_px"`
	LineHeight    float64 `json:"line_height"`
}

type Strokes struct {
	ThinPX           int     `json:"thin_px"`
	StrongPX         int     `json:"strong_px"`
	HighlightOpacity float64 `json:"highlight_opacity"`
}

type Radii struct {
	SmallPX   int `json:"small_px"`
	ControlPX int `json:"control_px"`
	CardPX    int `json:"card_px"`
	DialogPX  int `json:"dialog_px"`
	PillPX    int `json:"pill_px"`
}

type Spacing struct {
	XSPX      int `json:"xs_px"`
	SMPX      int `json:"sm_px"`
	MDPX      int `json:"md_px"`
	LGPX      int `json:"lg_px"`
	XLPX      int `json:"xl_px"`
	XXLPX     int `json:"xxl_px"`
	SectionPX int `json:"section_px"`
}

type Motion struct {
	FastMS         int    `json:"fast_ms"`
	NormalMS       int    `json:"normal_ms"`
	SlowMS         int    `json:"slow_ms"`
	EaseStandard   string `json:"ease_standard"`
	EaseEmphasized string `json:"ease_emphasized"`
}

// Load parses and validates the embedded canonical token document.
func Load() (Tokens, error) {
	var tokens Tokens
	if err := json.Unmarshal(JSON, &tokens); err != nil {
		return Tokens{}, fmt.Errorf("design tokens: %w", err)
	}
	if tokens.Colors.Accent.Cyan == "" ||
		tokens.Colors.Dark.Canvas == "" ||
		tokens.Colors.Dark.Warm == "" ||
		tokens.Colors.Dark.Success == "" ||
		tokens.Colors.Dark.Danger == "" ||
		tokens.Colors.Light.Canvas == "" ||
		tokens.Colors.Light.Warm == "" ||
		tokens.Colors.Light.Success == "" ||
		tokens.Colors.Light.Danger == "" ||
		tokens.Text.FontFamily == "" ||
		tokens.Radii.CardPX <= 0 ||
		tokens.Motion.NormalMS <= 0 {
		return Tokens{}, fmt.Errorf("design tokens: required values are missing")
	}
	return tokens, nil
}

// Default is the validated canonical token set.
var Default = mustLoad()

func mustLoad() Tokens {
	tokens, err := Load()
	if err != nil {
		panic(err)
	}
	return tokens
}
