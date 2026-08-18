package output

import (
	"bytes"
	"io"
	"unicode/utf8"
)

// TerminalMode controls human-renderer styling and final-byte sanitization. ANSI
// enables escape sequences at all; Color enables colored sequences. Sanitize
// strips terminal control bytes from the rendered human output.
type TerminalMode struct {
	ANSI     bool
	Color    bool
	Sanitize bool
}

// styled reports whether colored ANSI sequences should be emitted. Both fields
// must be set, so a zero TerminalMode is plain.
func (m TerminalMode) styled() bool { return m.ANSI && m.Color }

// RenderHuman buffers a human renderer and applies terminal-control
// sanitization when the selected mode requires it. A nil renderer produces no
// output.
func RenderHuman(mode TerminalMode, render func(io.Writer)) []byte {
	return RenderHumanWith(mode, render, nil)
}

// RenderHumanWith is RenderHuman with a transform applied to the buffered output
// before sanitization, so callers can rewrite bytes while they still hold their
// original, unsanitized form. A nil transform is a no-op.
func RenderHumanWith(mode TerminalMode, render func(io.Writer), transform func([]byte) []byte) []byte {
	if render == nil {
		return nil
	}
	var buf bytes.Buffer
	render(&buf)
	b := buf.Bytes()
	if transform != nil {
		b = transform(b)
	}
	if mode.Sanitize {
		return SanitizeControlBytes(b)
	}
	return b
}

// SanitizeControlBytes removes ANSI escape sequences and other terminal control
// bytes, preserving tabs and newlines. Removed control runs become one space
// separator so adjacent printable text does not get joined together.
func SanitizeControlBytes(in []byte) []byte {
	var out []byte
	for i := 0; i < len(in); {
		b := in[i]
		switch {
		case b == 0x1b:
			next := consumeEscape(in, i)
			out = appendSeparator(out)
			i = skipFollowingSpace(in, next)
		case b == 0x9b:
			next := consumeCSI(in, i+1)
			out = appendSeparator(out)
			i = skipFollowingSpace(in, next)
		case b == 0x9d:
			next := consumeStringControl(in, i+1)
			out = appendSeparator(out)
			i = skipFollowingSpace(in, next)
		case b == '\t' || b == '\n':
			out = append(out, b)
			i++
		case b == '\r':
			if i+1 < len(in) && in[i+1] == '\n' {
				i++
				continue
			}
			out = appendSeparator(out)
			i = skipFollowingSpace(in, i+1)
		case b >= utf8.RuneSelf:
			// Preserve valid UTF-8 as text. Standalone C1/control bytes in this
			// range fail decoding and are handled as control bytes below.
			if r, size := utf8.DecodeRune(in[i:]); r != utf8.RuneError || size > 1 {
				out = append(out, in[i:i+size]...)
				i += size
				continue
			}
			out = appendSeparator(out)
			i = skipFollowingSpace(in, i+1)
		case isControlByte(b):
			out = appendSeparator(out)
			i = skipFollowingSpace(in, i+1)
		default:
			out = append(out, b)
			i++
		}
	}
	return out
}

// consumeEscape returns the first byte after an ESC sequence. It handles the
// common CSI and string-control forms plus generic ESC sequences with
// intermediate bytes, such as charset selection.
func consumeEscape(in []byte, start int) int {
	i := start + 1
	if i >= len(in) {
		return i
	}
	switch in[i] {
	case '[':
		return consumeCSI(in, i+1)
	case ']':
		return consumeStringControl(in, i+1)
	case 'P', '^', '_', 'X':
		return consumeStringControl(in, i+1)
	default:
		if in[i] >= 0x20 && in[i] <= 0x2f {
			for i < len(in) && in[i] >= 0x20 && in[i] <= 0x2f {
				i++
			}
			if i < len(in) {
				i++
			}
			return i
		}
		return i + 1
	}
}

// consumeCSI returns the first byte after a CSI sequence, or len(in) when the
// sequence is incomplete.
func consumeCSI(in []byte, start int) int {
	for i := start; i < len(in); i++ {
		if in[i] >= 0x40 && in[i] <= 0x7e {
			return i + 1
		}
	}
	return len(in)
}

// consumeStringControl returns the first byte after an OSC/DCS-style string
// control sequence, recognizing BEL, ESC\, and 8-bit ST terminators.
func consumeStringControl(in []byte, start int) int {
	for i := start; i < len(in); i++ {
		switch in[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 < len(in) && in[i+1] == '\\' {
				return i + 2
			}
		case 0x9c:
			return i + 1
		}
	}
	return len(in)
}

// appendSeparator inserts at most one space between printable runs split by a
// removed control sequence.
func appendSeparator(out []byte) []byte {
	if len(out) == 0 {
		return append(out, ' ')
	}
	switch out[len(out)-1] {
	case ' ', '\t', '\n':
		return out
	default:
		return append(out, ' ')
	}
}

// skipFollowingSpace prevents a removed control sequence plus a literal
// following space from becoming two spaces.
func skipFollowingSpace(in []byte, i int) int {
	if i < len(in) && in[i] == ' ' {
		return i + 1
	}
	return i
}

// isControlByte reports terminal control bytes that should not reach
// escape-free output. Tabs and newlines are filtered before this helper.
func isControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f || (b >= 0x80 && b <= 0x9f)
}

// ANSI policy. These are the only escape sequences this package emits, and only
// the DRY RUN label and the mutation action word are ever wrapped in them.
const (
	ansiReset          = "\x1b[0m"
	ansiDryRun         = "\x1b[1;33m" // bold yellow - the dry-run label
	ansiMutationAction = "\x1b[1;32m" // bold green - the mutation action word
)
