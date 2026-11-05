package winfx

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	ModAlt      uint32 = 0x0001
	ModControl  uint32 = 0x0002
	ModShift    uint32 = 0x0004
	ModWin      uint32 = 0x0008
	ModNoRepeat uint32 = 0x4000
)

var (
	ErrEmptyHotkey    = errors.New("winfx: hotkey is empty")
	ErrInvalidHotkey  = errors.New("winfx: invalid hotkey")
	ErrReservedHotkey = errors.New("winfx: hotkey is reserved by Windows")
)

// Hotkey is a parsed global hotkey. ModNoRepeat is added by RegisterHotKey,
// rather than stored in Modifiers, so String remains a user-facing chord.
type Hotkey struct {
	Modifiers uint32
	Key       uint32
	KeyName   string
}

func (h Hotkey) String() string {
	var parts []string
	if h.Modifiers&ModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if h.Modifiers&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if h.Modifiers&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if h.Modifiers&ModWin != 0 {
		parts = append(parts, "Win")
	}
	if h.KeyName != "" {
		parts = append(parts, h.KeyName)
	}
	return strings.Join(parts, "+")
}

// ParseHotkey parses a case-insensitive chord such as Ctrl+Alt+L. It accepts
// letters, digits, F1-F24, navigation keys, numpad keys, and common media keys.
func ParseHotkey(chord string) (Hotkey, error) {
	chord = strings.TrimSpace(chord)
	if chord == "" {
		return Hotkey{}, ErrEmptyHotkey
	}

	tokens := strings.Split(chord, "+")
	var result Hotkey
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		if token == "" {
			return Hotkey{}, fmt.Errorf("%w: empty component in %q", ErrInvalidHotkey, chord)
		}
		if modifier, ok := parseModifier(token); ok {
			if result.Modifiers&modifier != 0 {
				return Hotkey{}, fmt.Errorf("%w: duplicate modifier %q", ErrInvalidHotkey, token)
			}
			result.Modifiers |= modifier
			continue
		}
		if result.Key != 0 {
			return Hotkey{}, fmt.Errorf("%w: multiple non-modifier keys", ErrInvalidHotkey)
		}
		key, name, ok := parseVirtualKey(token)
		if !ok {
			return Hotkey{}, fmt.Errorf("%w: unsupported key %q", ErrInvalidHotkey, token)
		}
		result.Key = key
		result.KeyName = name
	}
	if result.Key == 0 {
		return Hotkey{}, fmt.Errorf("%w: missing non-modifier key", ErrInvalidHotkey)
	}
	return result, nil
}

func parseModifier(token string) (uint32, bool) {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "ctrl", "control":
		return ModControl, true
	case "alt":
		return ModAlt, true
	case "shift":
		return ModShift, true
	case "win", "windows", "meta", "super":
		return ModWin, true
	default:
		return 0, false
	}
}

func parseVirtualKey(token string) (uint32, string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(token))
	if len(upper) == 1 {
		value := upper[0]
		if value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			return uint32(value), upper, true
		}
	}
	if strings.HasPrefix(upper, "F") {
		number, err := strconv.Atoi(strings.TrimPrefix(upper, "F"))
		if err == nil && number >= 1 && number <= 24 {
			return uint32(0x70 + number - 1), fmt.Sprintf("F%d", number), true
		}
	}

	type keySpec struct {
		vk   uint32
		name string
	}
	keys := map[string]keySpec{
		"backspace":         {0x08, "Backspace"},
		"back":              {0x08, "Backspace"},
		"tab":               {0x09, "Tab"},
		"enter":             {0x0D, "Enter"},
		"return":            {0x0D, "Enter"},
		"pause":             {0x13, "Pause"},
		"capslock":          {0x14, "CapsLock"},
		"escape":            {0x1B, "Escape"},
		"esc":               {0x1B, "Escape"},
		"space":             {0x20, "Space"},
		"pageup":            {0x21, "PageUp"},
		"pgup":              {0x21, "PageUp"},
		"prior":             {0x21, "PageUp"},
		"pagedown":          {0x22, "PageDown"},
		"pgdn":              {0x22, "PageDown"},
		"next":              {0x22, "PageDown"},
		"end":               {0x23, "End"},
		"home":              {0x24, "Home"},
		"left":              {0x25, "Left"},
		"up":                {0x26, "Up"},
		"right":             {0x27, "Right"},
		"down":              {0x28, "Down"},
		"printscreen":       {0x2C, "PrintScreen"},
		"print_screen":      {0x2C, "PrintScreen"},
		"snapshot":          {0x2C, "PrintScreen"},
		"prtsc":             {0x2C, "PrintScreen"},
		"prtscr":            {0x2C, "PrintScreen"},
		"insert":            {0x2D, "Insert"},
		"ins":               {0x2D, "Insert"},
		"delete":            {0x2E, "Delete"},
		"del":               {0x2E, "Delete"},
		"apps":              {0x5D, "Apps"},
		"menu":              {0x5D, "Apps"},
		"contextmenu":       {0x5D, "Apps"},
		"context_menu":      {0x5D, "Apps"},
		"application":       {0x5D, "Apps"},
		"sleep":             {0x5F, "Sleep"},
		"num0":              {0x60, "Num0"},
		"numpad0":           {0x60, "Num0"},
		"num1":              {0x61, "Num1"},
		"numpad1":           {0x61, "Num1"},
		"num2":              {0x62, "Num2"},
		"numpad2":           {0x62, "Num2"},
		"num3":              {0x63, "Num3"},
		"numpad3":           {0x63, "Num3"},
		"num4":              {0x64, "Num4"},
		"numpad4":           {0x64, "Num4"},
		"num5":              {0x65, "Num5"},
		"numpad5":           {0x65, "Num5"},
		"num6":              {0x66, "Num6"},
		"numpad6":           {0x66, "Num6"},
		"num7":              {0x67, "Num7"},
		"numpad7":           {0x67, "Num7"},
		"num8":              {0x68, "Num8"},
		"numpad8":           {0x68, "Num8"},
		"num9":              {0x69, "Num9"},
		"numpad9":           {0x69, "Num9"},
		"multiply":          {0x6A, "Multiply"},
		"add":               {0x6B, "Add"},
		"separator":         {0x6C, "Separator"},
		"numseparator":      {0x6C, "Separator"},
		"numpadseparator":   {0x6C, "Separator"},
		"subtract":          {0x6D, "Subtract"},
		"decimal":           {0x6E, "Decimal"},
		"divide":            {0x6F, "Divide"},
		"numlock":           {0x90, "NumLock"},
		"scrolllock":        {0x91, "ScrollLock"},
		"browserback":       {0xA6, "BrowserBack"},
		"browser_back":      {0xA6, "BrowserBack"},
		"browserforward":    {0xA7, "BrowserForward"},
		"browser_forward":   {0xA7, "BrowserForward"},
		"browserrefresh":    {0xA8, "BrowserRefresh"},
		"browser_refresh":   {0xA8, "BrowserRefresh"},
		"browserstop":       {0xA9, "BrowserStop"},
		"browser_stop":      {0xA9, "BrowserStop"},
		"browsersearch":     {0xAA, "BrowserSearch"},
		"browser_search":    {0xAA, "BrowserSearch"},
		"browserfavorites":  {0xAB, "BrowserFavorites"},
		"browser_favorites": {0xAB, "BrowserFavorites"},
		"browserhome":       {0xAC, "BrowserHome"},
		"browser_home":      {0xAC, "BrowserHome"},
		"volumemute":        {0xAD, "VolumeMute"},
		"volume_mute":       {0xAD, "VolumeMute"},
		"volumedown":        {0xAE, "VolumeDown"},
		"volume_down":       {0xAE, "VolumeDown"},
		"volumeup":          {0xAF, "VolumeUp"},
		"volume_up":         {0xAF, "VolumeUp"},
		"medianext":         {0xB0, "MediaNext"},
		"media_next":        {0xB0, "MediaNext"},
		"mediaprevious":     {0xB1, "MediaPrevious"},
		"media_previous":    {0xB1, "MediaPrevious"},
		"mediastop":         {0xB2, "MediaStop"},
		"media_stop":        {0xB2, "MediaStop"},
		"mediaplaypause":    {0xB3, "MediaPlayPause"},
		"media_play_pause":  {0xB3, "MediaPlayPause"},
		"launchmail":        {0xB4, "LaunchMail"},
		"launch_mail":       {0xB4, "LaunchMail"},
		"mail":              {0xB4, "LaunchMail"},
		"launchmedia":       {0xB5, "LaunchMediaSelect"},
		"launch_media":      {0xB5, "LaunchMediaSelect"},
		"launchmediaselect": {0xB5, "LaunchMediaSelect"},
		"media_select":      {0xB5, "LaunchMediaSelect"},
		"launchapp1":        {0xB6, "LaunchApp1"},
		"launch_app1":       {0xB6, "LaunchApp1"},
		"app1":              {0xB6, "LaunchApp1"},
		"launchapp2":        {0xB7, "LaunchApp2"},
		"launch_app2":       {0xB7, "LaunchApp2"},
		"app2":              {0xB7, "LaunchApp2"},
		"semicolon":         {0xBA, "Semicolon"},
		";":                 {0xBA, "Semicolon"},
		"equal":             {0xBB, "Equal"},
		"equals":            {0xBB, "Equal"},
		"=":                 {0xBB, "Equal"},
		"comma":             {0xBC, "Comma"},
		",":                 {0xBC, "Comma"},
		"minus":             {0xBD, "Minus"},
		"-":                 {0xBD, "Minus"},
		"period":            {0xBE, "Period"},
		"dot":               {0xBE, "Period"},
		".":                 {0xBE, "Period"},
		"slash":             {0xBF, "Slash"},
		"/":                 {0xBF, "Slash"},
		"backtick":          {0xC0, "Backtick"},
		"grave":             {0xC0, "Backtick"},
		"`":                 {0xC0, "Backtick"},
		"leftbracket":       {0xDB, "LeftBracket"},
		"left_bracket":      {0xDB, "LeftBracket"},
		"[":                 {0xDB, "LeftBracket"},
		"backslash":         {0xDC, "Backslash"},
		"\\":                {0xDC, "Backslash"},
		"rightbracket":      {0xDD, "RightBracket"},
		"right_bracket":     {0xDD, "RightBracket"},
		"]":                 {0xDD, "RightBracket"},
		"quote":             {0xDE, "Quote"},
		"apostrophe":        {0xDE, "Quote"},
		"'":                 {0xDE, "Quote"},
	}
	spec, ok := keys[strings.ToLower(strings.TrimSpace(token))]
	if !ok {
		return 0, "", false
	}
	return spec.vk, spec.name, true
}
