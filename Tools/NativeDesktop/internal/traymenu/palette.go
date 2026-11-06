package traymenu

import "pccontroller.local/desktop/internal/design"

type rgb struct {
	r byte
	g byte
	b byte
}

type palette struct {
	background rgb
	border     rgb
	text       rgb
	secondary  rgb
	disabled   rgb
	hover      rgb
	separator  rgb
	check      rgb
}

func paletteFor(dark bool) palette {
	if dark {
		tokens := design.Default.Colors.Dark
		return palette{
			background: tokenRGB(tokens.SurfaceOverlay),
			border:     tokenRGB(tokens.Stroke),
			text:       tokenRGB(tokens.Text),
			secondary:  tokenRGB(tokens.TextMuted),
			disabled:   tokenRGB(tokens.TextSubtle),
			hover:      tokenRGB(tokens.SurfaceHigh),
			separator:  tokenRGB(tokens.Stroke),
			check:      tokenRGB(tokens.Accent),
		}
	}
	tokens := design.Default.Colors.Light
	return palette{
		background: tokenRGB(tokens.SurfaceOverlay),
		border:     tokenRGB(tokens.Stroke),
		text:       tokenRGB(tokens.Text),
		secondary:  tokenRGB(tokens.TextMuted),
		disabled:   tokenRGB(tokens.TextSubtle),
		hover:      tokenRGB(tokens.SurfaceHigh),
		separator:  tokenRGB(tokens.Stroke),
		check:      tokenRGB(tokens.Accent),
	}
}

func tokenRGB(value string) rgb {
	color := design.MustColor(value)
	return rgb{r: color.R, g: color.G, b: color.B}
}
