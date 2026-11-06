package design

// ControllerTokens adapts the complete donor design system to PCController's
// WebUI brand. Call before creating UI workers; standalone donor defaults stay
// unchanged. Fixtures use this same source instead of a separate blue theme.
func ControllerTokens() Tokens {
	t := Default
	t.Colors.Dark = ThemePalette{
		Accent: "#a78bfa", AccentHover: "#d8ccff", Warm: "#e7ae63", Success: "#73c69a", Danger: "#ed7889",
		Canvas: "#121116", CanvasAlt: "#0b0a0e", Surface: "#1d1b22", SurfaceHigh: "#33303b", SurfaceOverlay: "#33303b",
		Text: "#f5f2f7", TextMuted: "#938c9b", TextSubtle: "#938c9b", Stroke: "#4a4353", StrokeHighlight: "#70657f", Shadow: "#000000",
	}
	t.Colors.Light = ThemePalette{
		Accent: "#6845bf", AccentHover: "#5334a3", Warm: "#8a5917", Success: "#307752", Danger: "#b33f53",
		Canvas: "#f5f3f6", CanvasAlt: "#ebe8ed", Surface: "#ffffff", SurfaceHigh: "#ded8e5", SurfaceOverlay: "#ebe8ed",
		Text: "#242126", TextMuted: "#7b7382", TextSubtle: "#7b7382", Stroke: "#c2b9cc", StrokeHighlight: "#ffffff", Shadow: "#000000",
	}
	t.Text.FontFamily = "Segoe UI"
	return t
}
