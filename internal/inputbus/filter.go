package inputbus

import "bytes"

const (
	esc = 0x1b
	bel = 0x07
)

// StripReplies removes what a terminal writes on its own behalf from a chunk
// of its input stream, and returns the rest, which is what the user typed or
// pasted. MyGo's terminal sends everything through one writer: keys, pastes,
// mouse reports, focus reports, and the answers it gives to programs'
// questions (cursor position, device attributes, colors, size, mode reports).
// Broadcasting the answers would feed one machine's screen state to every
// other synced machine, so they are dropped here.
//
// The terminal pushes each encoded key or answer as its own chunk, so
// sequences are not split across calls; a truncated sequence at the end of a
// chunk is kept as typed input.
func StripReplies(p []byte) []byte {
	if !bytes.Contains(p, []byte{esc}) {
		return p
	}
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); {
		if p[i] != esc {
			out = append(out, p[i])
			i++
			continue
		}
		end, reply := classify(p[i:])
		if !reply {
			out = append(out, p[i:i+end]...)
		}
		i += end
	}
	return out
}

// classify reads one escape sequence at the start of p and returns its
// length and whether it is something the terminal generated itself.
func classify(p []byte) (length int, reply bool) {
	if len(p) < 2 {
		return len(p), false
	}
	switch p[1] {
	case '[':
		return classifyCSI(p)
	case ']', 'P', '_', '^', 'X':
		// OSC, DCS, APC, PM, SOS: string sequences end with BEL or ST.
		return stringEnd(p), true
	}
	// ESC followed by a character is an Alt-modified key.
	return 2, false
}

func classifyCSI(p []byte) (length int, reply bool) {
	i := 2
	for i < len(p) && p[i] >= 0x30 && p[i] <= 0x3f { // parameter bytes
		i++
	}
	paramEnd := i
	for i < len(p) && p[i] >= 0x20 && p[i] <= 0x2f { // intermediate bytes
		i++
	}
	if i >= len(p) {
		return len(p), false
	}
	final := p[i]
	length = i + 1
	params := p[2:paramEnd]
	private := len(params) > 0 && (params[0] == '?' || params[0] == '>' || params[0] == '=')
	hasInter := i > paramEnd

	switch {
	case final == 'I' || final == 'O':
		// Focus in and out reports belong to the window that has the focus.
		return length, len(params) == 0
	case final == 'M' && len(params) == 0:
		// X10 mouse report: three raw bytes follow.
		return min(length+3, len(p)), true
	case (final == 'M' || final == 'm') && len(params) > 0 && params[0] == '<':
		return length, true // SGR mouse report
	case final == 'c' && private:
		return length, true // device attributes
	case final == 'R' && (private || bytes.Contains(params, []byte{';'}) && !hasInter && cursorReport(params)):
		return length, true // cursor position report
	case final == 'y' && hasInter:
		return length, true // DECRPM mode report: CSI ? Ps ; Pm $ y
	case final == 'n' && private:
		return length, true // CSI ? 997 ; Ps n color scheme report, DSR answers
	case final == 't' && !private && len(params) > 0 && params[0] >= '4' && params[0] <= '9':
		return length, true // window size reports (CSI 4/8/9 ; ... t)
	}
	return length, false
}

// cursorReport tells a cursor position report (CSI row ; col R) from a
// Shift+F3 key (CSI 1 ; modifier R, legacy encoding) by its row: a real report
// can also have row 1, so this is a heuristic that keeps modified F3 working
// and may let a row-1 report through; see ADR 0007 and the InputBus section of docs/TECH-PLAN.md.
func cursorReport(params []byte) bool {
	row, _, ok := bytes.Cut(params, []byte{';'})
	return ok && !bytes.Equal(row, []byte{'1'})
}

// stringEnd returns the length of an OSC/DCS/APC string starting at p[0],
// ended by BEL or ST (ESC \). An unterminated string consumes the chunk.
func stringEnd(p []byte) int {
	for i := 2; i < len(p); i++ {
		switch {
		case p[i] == bel:
			return i + 1
		case p[i] == esc && i+1 < len(p) && p[i+1] == '\\':
			return i + 2
		}
	}
	return len(p)
}
