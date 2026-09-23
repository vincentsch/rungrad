// Package choose renders an interactive single-choice question.
//
// On a real terminal the user moves the highlight with the arrow keys (or j/k,
// a digit, or an option's one-letter alias) and confirms with Enter. Nothing is
// ever chosen without Enter, so a stray keypress, a function key or pasted text
// cannot pick an answer. Anywhere raw input is not available (pipes, tests,
// TERM=dumb, Windows consoles, callers that set Plain) the same question is
// rendered as a numbered list read line by line.
//
// Choose never decides whether a prompt is allowed. Commands must still check
// their non-interactive policy first and offer a flag that answers the
// question for automated use.
package choose

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// ErrCanceled reports that the user backed out (q, Ctrl-C, Ctrl-D) or that
// input ended before an answer was given.
var ErrCanceled = errors.New("choice canceled")

// Option is one answer. Hint is an optional short explanation shown dimmed.
// Aliases are extra answers accepted when typed in full in line mode, such as
// "y" or "no"; in the arrow-key menu a one-character alias moves the highlight
// to its option (Enter still confirms).
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
	// honouring --no-ansi.
	Plain bool
	// NoColor keeps the arrow-key menu but drops colour and bold, for callers
	// honouring --no-color or NO_COLOR.
	NoColor bool
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
	if in, out, ok := c.rawCapable(); ok {
		return c.chooseRaw(in, out, question, options, def)
	}
	return c.chooseLines(question, options, def)
}

// Confirm asks a two-way question with explicit labels, for example
// ("Log in again", "Keep the current login"). The no option is highlighted
// first, so pressing Enter straight away is always the safe answer; y/yes and
// n/no are accepted as aliases.
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

func (c Chooser) rawCapable() (int, int, bool) {
	if c.Plain || runtime.GOOS == "windows" || os.Getenv("TERM") == "dumb" {
		return 0, 0, false
	}
	in, ok := c.In.(*os.File)
	if !ok || !term.IsTerminal(int(in.Fd())) {
		return 0, 0, false
	}
	out, ok := c.Out.(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return 0, 0, false
	}
	return int(in.Fd()), int(out.Fd()), true
}

func (c Chooser) text(s string) string {
	if c.Transform != nil {
		return c.Transform(s)
	}
	return s
}

// chooseLines is the portable fallback: a numbered list and a typed answer.
// Only an exact option number, an exact alias or a full label is accepted, so
// an unrelated line (for example a pasted command) asks again rather than
// being read as an answer. If the question cannot be written, no answer is
// read, so input is never consumed for a question the user did not see.
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
		if idx, ok := matchAnswer(answer, options); ok {
			return idx, nil
		}
	}
}

// matchAnswer accepts an option number, an alias or a full label, compared
// case-insensitively. Partial labels are deliberately not accepted.
func matchAnswer(answer string, options []Option) (int, bool) {
	if n, err := strconv.Atoi(answer); err == nil {
		if n >= 1 && n <= len(options) {
			return n - 1, true
		}
		return 0, false
	}
	for i, opt := range options {
		if strings.EqualFold(opt.Label, answer) {
			return i, true
		}
		for _, alias := range opt.Aliases {
			if strings.EqualFold(alias, answer) {
				return i, true
			}
		}
	}
	return 0, false
}

func (c Chooser) chooseRaw(inFD, outFD int, question string, options []Option, def int) (int, error) {
	state, err := term.MakeRaw(inFD)
	if err != nil {
		return c.chooseLines(question, options, def)
	}
	restore := func() {
		term.Restore(inFD, state)
		fmt.Fprint(c.Out, "\x1b[?2004l\x1b[?25h") // bracketed paste off, cursor on
	}
	defer restore()
	stopSignals := restoreOnSignal(restore)
	defer stopSignals()

	// Anything typed or pasted before the menu appeared is not an answer.
	discardPendingInput(inFD)

	// Hide the cursor while the menu is live, and ask the terminal to mark
	// pasted text so a paste can never move the highlight or press Enter.
	fmt.Fprint(c.Out, "\x1b[?25l\x1b[?2004h")

	width := func() int {
		if w, _, err := term.GetSize(outFD); err == nil && w > 0 {
			return w
		}
		return 80
	}

	m := model{cursor: def, count: len(options)}
	rows, err := c.render(question, options, m.cursor, width())
	if err != nil {
		return 0, fmt.Errorf("write choice prompt: %w", err)
	}
	keys := newKeyReader(c.In)
	for {
		k, raw, err := keys.next()
		if err != nil {
			c.clear(rows)
			if errors.Is(err, io.EOF) {
				return 0, ErrCanceled
			}
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
			c.clear(rows)
			fmt.Fprint(c.Out, c.text(question)+" "+c.dim("canceled")+"\r\n")
			return 0, ErrCanceled
		case m.chosen:
			c.clear(rows)
			fmt.Fprint(c.Out, c.text(question)+" "+c.bold(c.text(options[m.cursor].Label))+"\r\n")
			return m.cursor, nil
		}
		c.clear(rows)
		if rows, err = c.render(question, options, m.cursor, width()); err != nil {
			return 0, fmt.Errorf("write choice prompt: %w", err)
		}
	}
}

func (c Chooser) bold(s string) string {
	if c.NoColor {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

func (c Chooser) dim(s string) string {
	if c.NoColor {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

// render draws the menu and returns how many terminal rows it occupies,
// counting wrapped lines, so the next frame can erase exactly that many.
// Raw mode needs explicit carriage returns.
func (c Chooser) render(question string, options []Option, cursor, width int) (int, error) {
	lines := []string{c.text(question)}
	for i, opt := range options {
		marker, label := "  ", c.text(opt.Label)
		if i == cursor {
			marker = "> "
			if !c.NoColor {
				marker = "\x1b[36m›\x1b[0m "
			}
			label = c.bold(label)
		}
		line := marker + label
		if opt.Hint != "" {
			line += "  " + c.dim(c.text(opt.Hint))
		}
		lines = append(lines, line)
	}
	lines = append(lines, c.dim("↑/↓ to move, Enter to choose, q to cancel"))

	var b strings.Builder
	rows := 0
	for _, line := range lines {
		b.WriteString(line + "\r\n")
		rows += wrappedRows(line, width)
	}
	_, err := io.WriteString(c.Out, b.String())
	return rows, err
}

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// wrappedRows reports how many terminal rows a line occupies at width columns.
func wrappedRows(line string, width int) int {
	n := utf8.RuneCountInString(ansiSequence.ReplaceAllString(line, ""))
	if width <= 0 || n <= width {
		return 1
	}
	return (n + width - 1) / width
}

func (c Chooser) clear(rows int) {
	for i := 0; i < rows; i++ {
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
	keyDigit1 // keyDigit1 + n moves the highlight to option n+1
)

// model is the pure selection state, kept separate from terminal I/O so it can
// be tested without a TTY. Only Enter chooses; every other key only moves the
// highlight, and the highlight stops at the ends instead of wrapping.
type model struct {
	cursor   int
	count    int
	chosen   bool
	canceled bool
}

func (m model) step(k key) model {
	switch {
	case k == keyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case k == keyDown:
		if m.cursor < m.count-1 {
			m.cursor++
		}
	case k == keyEnter:
		m.chosen = true
	case k == keyCancel:
		m.canceled = true
	case k >= keyDigit1:
		if n := int(k - keyDigit1); n < m.count {
			m.cursor = n
		}
	}
	return m
}

type keyReader struct {
	in      io.Reader
	pending []byte
}

func newKeyReader(in io.Reader) *keyReader { return &keyReader{in: in} }

func (r *keyReader) byte() (byte, error) {
	if len(r.pending) > 0 {
		b := r.pending[0]
		r.pending = r.pending[1:]
		return b, nil
	}
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

func (r *keyReader) unread(b byte) { r.pending = append([]byte{b}, r.pending...) }

// next decodes one key and also returns the byte that started it, so callers
// can map plain letters to option aliases.
func (r *keyReader) next() (key, byte, error) {
	b, err := r.byte()
	if err != nil {
		return keyNone, 0, err
	}
	switch b {
	case '\r', '\n':
		return keyEnter, b, nil
	case 3, 4, 'q', 'Q': // Ctrl-C, Ctrl-D
		return keyCancel, b, nil
	case 'k', 'K':
		return keyUp, b, nil
	case 'j', 'J':
		return keyDown, b, nil
	case 0x1b:
		k, err := r.escape()
		return k, b, err
	}
	if b >= '1' && b <= '9' {
		return keyDigit1 + key(b-'1'), b, nil
	}
	return keyNone, b, nil
}

// escape consumes a whole escape sequence so none of its bytes are read as
// separate keys: CSI (ESC [ params final), SS3 (ESC O x), or ESC followed by an
// unrelated byte, which is put back. Only plain up and down arrows move.
func (r *keyReader) escape() (key, error) {
	second, err := r.byte()
	if err != nil {
		return keyNone, err
	}
	switch second {
	case 'O':
		third, err := r.byte()
		if err != nil {
			return keyNone, err
		}
		return arrow(third, ""), nil
	case '[':
		var params []byte
		for {
			b, err := r.byte()
			if err != nil {
				return keyNone, err
			}
			if b >= 0x40 && b <= 0x7e { // final byte
				if b == '~' && string(params) == "200" {
					return keyNone, r.skipPaste()
				}
				return arrow(b, string(params)), nil
			}
			params = append(params, b)
			if len(params) > 16 { // not a sequence we understand; stop eating
				return keyNone, nil
			}
		}
	default:
		// A lone Esc followed by an ordinary key: keep that key.
		r.unread(second)
		return keyNone, nil
	}
}

// skipPaste discards a bracketed paste up to and including its end marker
// ESC [ 2 0 1 ~, so nothing pasted is read as a key.
func (r *keyReader) skipPaste() error {
	const end = "\x1b[201~"
	matched := 0
	for {
		b, err := r.byte()
		if err != nil {
			return err
		}
		switch {
		case b == end[matched]:
			matched++
			if matched == len(end) {
				return nil
			}
		case b == end[0]:
			matched = 1
		default:
			matched = 0
		}
	}
}

func arrow(final byte, params string) key {
	if params != "" {
		return keyNone // Shift/Ctrl-modified arrows, F-keys and others
	}
	switch final {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	}
	return keyNone
}

// aliasKey maps a single keypress to the option whose one-character alias it
// is, for example y and n in a yes/no question. It only moves the highlight.
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
