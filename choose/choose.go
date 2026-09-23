// Package choose renders an interactive single-choice question.
//
// On a real terminal the user moves with the arrow keys (or j/k) and confirms
// with Enter; digits pick an option directly. Anywhere raw input is not
// available (pipes, tests, dumb terminals, Windows consoles without VT input)
// the same question is rendered as a numbered list read line by line, so a
// caller never has to special-case the environment.
//
// Choose never decides whether a prompt is allowed. Commands must still check
// their non-interactive policy first and offer a flag such as --force for
// automated use.
package choose

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrCanceled reports that the user backed out with q or Ctrl-C.
var ErrCanceled = errors.New("choice canceled")

// Option is one answer. Hint is an optional short explanation shown dimmed.
// Aliases are extra answers typed in full in line mode, such as "y" or "no";
// a single-character alias also selects the option with one keypress in the
// arrow-key menu.
type Option struct {
	Label   string
	Hint    string
	Aliases []string
}

// Chooser holds the streams a question is asked on.
type Chooser struct {
	In  io.Reader
	Out io.Writer
	// Plain forces the numbered line mode even on a terminal, for callers
	// honouring --no-ansi or NO_COLOR style preferences.
	Plain bool
	// Transform, when set, is applied to every rendered string, for example to
	// redact secrets before they reach the terminal.
	Transform func(string) string
}

// Choose asks question and returns the index of the selected option. def is
// the option highlighted first and the answer for a blank line in line mode.
func (c Chooser) Choose(question string, options []Option, def int) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("choose: no options provided")
	}
	if def < 0 || def >= len(options) {
		def = 0
	}
	if fd, ok := c.rawCapable(); ok {
		return c.chooseRaw(fd, question, options, def)
	}
	return c.chooseLines(question, options, def)
}

// Confirm asks a two-way question with explicit labels, for example
// ("Log in again", "Keep the current login"). The no option is highlighted
// first, so pressing Enter straight away is always the safe answer.
func (c Chooser) Confirm(question, yes, no string) (bool, error) {
	idx, err := c.Choose(question, []Option{
		{Label: yes, Aliases: []string{"y", "yes"}},
		{Label: no, Aliases: []string{"n", "no"}},
	}, 1)
	if err != nil {
		return false, err
	}
	return idx == 0, nil
}

func (c Chooser) rawCapable() (int, bool) {
	if c.Plain || runtime.GOOS == "windows" {
		return 0, false
	}
	in, ok := c.In.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return 0, false
	}
	out, ok := c.Out.(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return 0, false
	}
	if os.Getenv("TERM") == "dumb" {
		return 0, false
	}
	return int(in.Fd()), true
}

func (c Chooser) text(s string) string {
	if c.Transform != nil {
		return c.Transform(s)
	}
	return s
}

// chooseLines is the portable fallback: a numbered list and a typed answer.
// If the question cannot be written, it returns that error without reading,
// so an answer is never consumed for a question the user did not see.
func (c Chooser) chooseLines(question string, options []Option, def int) (int, error) {
	var b strings.Builder
	b.WriteString(c.text(question) + "\n")
	for i, opt := range options {
		line := fmt.Sprintf("  %d) %s", i+1, opt.Label)
		if opt.Hint != "" {
			line += "  (" + opt.Hint + ")"
		}
		b.WriteString(c.text(line) + "\n")
	}
	b.WriteString(fmt.Sprintf("Choose 1-%d [%d]: ", len(options), def+1))
	menu := b.String()

	for {
		if _, err := io.WriteString(c.Out, menu); err != nil {
			return 0, fmt.Errorf("write choice prompt: %w", err)
		}
		line, err := readLine(c.In)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, ErrCanceled
			}
			return 0, err
		}
		answer := strings.TrimSpace(line)
		if answer == "" {
			return def, nil
		}
		if n, convErr := strconv.Atoi(answer); convErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		if idx, ok := matchLabel(answer, options); ok {
			return idx, nil
		}
	}
}

// matchLabel accepts an exact alias, or an answer that starts exactly one
// label, so a habitual "y" or "n" still works for yes/no style options.
func matchLabel(answer string, options []Option) (int, bool) {
	answer = strings.ToLower(answer)
	for i, opt := range options {
		for _, alias := range opt.Aliases {
			if strings.ToLower(alias) == answer {
				return i, true
			}
		}
	}
	found := -1
	for i, opt := range options {
		if strings.HasPrefix(strings.ToLower(opt.Label), answer) {
			if found >= 0 {
				return 0, false
			}
			found = i
		}
	}
	return found, found >= 0
}

func (c Chooser) chooseRaw(fd int, question string, options []Option, def int) (int, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return c.chooseLines(question, options, def)
	}
	defer term.Restore(fd, state)

	m := model{cursor: def, count: len(options)}
	fmt.Fprint(c.Out, "\x1b[?25l") // hide cursor while the menu is live
	defer fmt.Fprint(c.Out, "\x1b[?25h")

	lines, err := c.render(question, options, m.cursor)
	if err != nil {
		return 0, fmt.Errorf("write choice prompt: %w", err)
	}
	keys := newKeyReader(c.In)
	for {
		k, raw, err := keys.nextRaw()
		if err != nil {
			c.clear(lines)
			return 0, err
		}
		if k == keyNone {
			if idx, ok := aliasKey(raw, options); ok {
				k = keyDigit1 + key(idx)
			}
		}
		m = m.step(k)
		switch {
		case m.canceled:
			c.clear(lines)
			fmt.Fprint(c.Out, c.text(question)+" \x1b[2mcanceled\x1b[0m\r\n")
			return 0, ErrCanceled
		case m.chosen:
			c.clear(lines)
			fmt.Fprint(c.Out, c.text(question)+" \x1b[1m"+c.text(options[m.cursor].Label)+"\x1b[0m\r\n")
			return m.cursor, nil
		}
		c.clear(lines)
		if lines, err = c.render(question, options, m.cursor); err != nil {
			return 0, fmt.Errorf("write choice prompt: %w", err)
		}
	}
}

// render draws the menu and returns how many lines it used, so the next frame
// can erase exactly that many. Raw mode needs explicit carriage returns.
func (c Chooser) render(question string, options []Option, cursor int) (int, error) {
	var b strings.Builder
	b.WriteString(c.text(question) + "\r\n")
	for i, opt := range options {
		marker, style, reset := "  ", "", ""
		if i == cursor {
			marker, style, reset = "\x1b[36m›\x1b[0m ", "\x1b[1m", "\x1b[0m"
		}
		line := marker + style + c.text(opt.Label) + reset
		if opt.Hint != "" {
			line += "  \x1b[2m" + c.text(opt.Hint) + "\x1b[0m"
		}
		b.WriteString(line + "\r\n")
	}
	b.WriteString("\x1b[2m↑/↓ to move, Enter to choose, q to cancel\x1b[0m\r\n")
	_, err := io.WriteString(c.Out, b.String())
	return len(options) + 2, err
}

func (c Chooser) clear(lines int) {
	for i := 0; i < lines; i++ {
		fmt.Fprint(c.Out, "\x1b[1A\x1b[2K")
	}
	fmt.Fprint(c.Out, "\r")
}

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
	keyDigit1 // keyDigit1 + n selects option n+1
)

// model is the pure selection state, kept separate from terminal I/O so it can
// be tested without a TTY.
type model struct {
	cursor   int
	count    int
	chosen   bool
	canceled bool
}

func (m model) step(k key) model {
	switch {
	case k == keyUp:
		m.cursor = (m.cursor - 1 + m.count) % m.count
	case k == keyDown:
		m.cursor = (m.cursor + 1) % m.count
	case k == keyEnter:
		m.chosen = true
	case k == keyCancel:
		m.canceled = true
	case k >= keyDigit1:
		if n := int(k - keyDigit1); n < m.count {
			m.cursor = n
			m.chosen = true
		}
	}
	return m
}

type keyReader struct {
	in io.Reader
}

func newKeyReader(in io.Reader) *keyReader { return &keyReader{in: in} }

func (r *keyReader) byte() (byte, error) {
	var one [1]byte
	for {
		n, err := r.in.Read(one[:])
		if n == 1 {
			return one[0], nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func (r *keyReader) next() (key, error) {
	k, _, err := r.nextRaw()
	return k, err
}

// nextRaw also returns the byte read, so callers can map plain letters to
// option aliases.
func (r *keyReader) nextRaw() (key, byte, error) {
	b, err := r.byte()
	if err != nil {
		return keyNone, 0, err
	}
	k, err := r.decode(b)
	return k, b, err
}

// aliasKey maps a single keypress to the option whose one-character alias it
// is, for example y and n in a yes/no question.
func aliasKey(b byte, options []Option) (int, bool) {
	for i, opt := range options {
		for _, alias := range opt.Aliases {
			if len(alias) == 1 && strings.EqualFold(alias, string(b)) {
				return i, true
			}
		}
	}
	return 0, false
}

func (r *keyReader) decode(b byte) (key, error) {
	switch b {
	case '\r', '\n':
		return keyEnter, nil
	case 3, 'q', 'Q': // Ctrl-C
		return keyCancel, nil
	case 'k', 'K':
		return keyUp, nil
	case 'j', 'J':
		return keyDown, nil
	case 0x1b:
		// Arrow keys arrive as ESC [ A/B or ESC O A/B. A bare Esc is not used
		// for cancel because reading past it would block on some terminals.
		second, err := r.byte()
		if err != nil {
			return keyNone, err
		}
		if second != '[' && second != 'O' {
			return keyNone, nil
		}
		third, err := r.byte()
		if err != nil {
			return keyNone, err
		}
		switch third {
		case 'A':
			return keyUp, nil
		case 'B':
			return keyDown, nil
		}
		return keyNone, nil
	}
	if b >= '1' && b <= '9' {
		return keyDigit1 + key(b-'1'), nil
	}
	return keyNone, nil
}

func readLine(in io.Reader) (string, error) {
	var buf []byte
	var one [1]byte
	for {
		n, err := in.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				return strings.TrimSuffix(string(buf), "\r"), nil
			}
			buf = append(buf, one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(buf) > 0 {
				return strings.TrimSuffix(string(buf), "\r"), nil
			}
			return "", err
		}
	}
}
