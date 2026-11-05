package deviceterminal

const (
	telnetIAC  = byte(255)
	telnetDONT = byte(254)
	telnetDO   = byte(253)
	telnetWONT = byte(252)
	telnetWILL = byte(251)
	telnetSB   = byte(250)
	telnetSE   = byte(240)
)

// telnetDecoder removes Telnet command bytes while retaining application
// bytes exactly. The audited firmware is a raw character stream that emits a
// single IAC WILL ECHO triplet but has no input-side IAC parser, so this client
// intentionally does not send negotiation replies that the firmware would
// mistake for shell input.
type telnetDecoder struct {
	state telnetDecodeState
}

type telnetDecodeState uint8

const (
	telnetData telnetDecodeState = iota
	telnetCommand
	telnetOption
	telnetSubnegotiation
	telnetSubnegotiationIAC
)

func (d *telnetDecoder) Feed(input []byte) []byte {
	if len(input) == 0 {
		return nil
	}
	output := make([]byte, 0, len(input))
	for _, value := range input {
		switch d.state {
		case telnetData:
			if value == telnetIAC {
				d.state = telnetCommand
			} else {
				output = append(output, value)
			}
		case telnetCommand:
			switch value {
			case telnetIAC:
				output = append(output, telnetIAC)
				d.state = telnetData
			case telnetWILL, telnetWONT, telnetDO, telnetDONT:
				d.state = telnetOption
			case telnetSB:
				d.state = telnetSubnegotiation
			default:
				// Two-byte Telnet commands have no application payload.
				d.state = telnetData
			}
		case telnetOption:
			// The option byte completes WILL/WONT/DO/DONT.
			d.state = telnetData
		case telnetSubnegotiation:
			if value == telnetIAC {
				d.state = telnetSubnegotiationIAC
			}
		case telnetSubnegotiationIAC:
			switch value {
			case telnetSE:
				d.state = telnetData
			case telnetIAC:
				d.state = telnetSubnegotiation
			default:
				d.state = telnetSubnegotiation
			}
		}
	}
	return output
}

// terminalFilter drops OSC/DCS/SOS/PM/APC strings. Those sequences can carry
// clipboard data, hyperlinks, or window-title requests and are unnecessary
// for the lamp's ANSI shell. Ordinary CSI cursor/color/erase sequences, BEL,
// CR/LF, and printable bytes are preserved for a real terminal emulator.
type terminalFilter struct {
	state terminalFilterState
}

type terminalFilterState uint8

const (
	filterData terminalFilterState = iota
	filterEscape
	filterControlString
	filterControlStringEscape
)

func (f *terminalFilter) Feed(input []byte) []byte {
	if len(input) == 0 {
		return nil
	}
	output := make([]byte, 0, len(input))
	for _, value := range input {
		switch f.state {
		case filterData:
			switch value {
			case 0x1b:
				f.state = filterEscape
			case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
				// 8-bit DCS/SOS/OSC/PM/APC.
				f.state = filterControlString
			default:
				output = append(output, value)
			}
		case filterEscape:
			switch value {
			case ']', 'P', 'X', '^', '_':
				f.state = filterControlString
			default:
				// Preserve ordinary ANSI escapes, including CSI and the
				// firmware's ESC 7/8 or ESC[s/u cursor save/restore forms.
				output = append(output, 0x1b, value)
				f.state = filterData
			}
		case filterControlString:
			switch value {
			case 0x07, 0x18, 0x1a, 0x9c:
				// BEL and ST terminate control strings. ECMA-48 CAN and
				// SUB abort an in-progress control sequence; treating them as
				// terminators prevents one malformed OSC/DCS payload from
				// swallowing the remainder of the terminal session.
				f.state = filterData
			case 0x1b:
				f.state = filterControlStringEscape
			}
		case filterControlStringEscape:
			if value == '\\' || value == 0x18 || value == 0x1a {
				f.state = filterData
			} else if value != 0x1b {
				f.state = filterControlString
			}
		}
	}
	return output
}
