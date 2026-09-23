// Package choose renders an interactive single-choice question.
//
// On a real terminal the user moves the highlight with the up and down arrow
// keys and confirms with Enter. No other key moves it, so typed or pasted text,
// including its newline, can only ever pick the preselected answer; callers
// should preselect the safe one. Anywhere raw input is not available (pipes,
// tests, TERM=dumb, Windows consoles, callers that set Plain) the same question
// is rendered as a numbered list that accepts only an option number, an alias
// or a full label, followed by Enter.
//
// While the arrow-key menu is open, SIGTERM, SIGHUP, SIGQUIT and SIGINT restore
// the terminal and are then re-raised with their default behaviour, which takes
// precedence over handlers the application installed for those signals.
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

	"golang.org/x/term"
)

// ErrCanceled reports that the user backed out (q, or Ctrl-C and Ctrl-D in the
// arrow-key menu) or that input ended before an answer was given. In line mode
// Ctrl-C is an ordinary SIGINT.
var ErrCanceled = errors.New("choice canceled")

// Option is one answer. Hint is an optional short explanation shown dimmed.
// Aliases are extra answers accepted when typed in full in line mode, such as
// "y" or "no". The arrow-key menu ignores them on purpose.
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
// the option preselected in the menu and the answer for a blank line in line
// mode; it must be a valid index, and should be the safe answer.
func (c Chooser) Choose(question string, options []Option, def int) (int, error) {
	if len(options) == 0 {
		return 0, errors.New("choose: no options provided")
	}
	if def < 0 || def >= len(options) {
		return 0, fmt.Errorf("choose: default %d is not one of the %d options", def, len(options))
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
		if strings.EqualFold(answer, "q") && !isAnswer(answer, options) {
			return 0, ErrCanceled
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
		if n >= 1 && n <= len(options) && strconv.Itoa(n) == answer {
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

// isAnswer reports whether text is a label or alias, so a literal "q" option
// is not mistaken for cancel.
func isAnswer(text string, options []Option) bool {
	for _, opt := range options {
		if strings.EqualFold(opt.Label, text) {
			return true
		}
		for _, alias := range opt.Aliases {
			if strings.EqualFold(alias, text) {
				return true
			}
		}
	}
	return false
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
	stopSignals := restoreOnSignal(restore)
	// Restore before the signal watcher stops, so a signal landing in between
	// still finds the terminal back in its normal state.
	defer func() {
		restore()
		stopSignals()
	}()

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
	var redrawErr error
	m, err = decide(newKeyReader(c.In), m, func(m model) {
		c.clear(rows)
		if rows, redrawErr = c.render(question, options, m.cursor, width()); redrawErr != nil {
			redrawErr = fmt.Errorf("write choice prompt: %w", redrawErr)
		}
	})
	c.clear(rows)
	switch {
	case redrawErr != nil:
		return 0, redrawErr
	case err != nil && errors.Is(err, io.EOF):
		return 0, ErrCanceled
	case err != nil:
		return 0, err
	case m.canceled:
		fmt.Fprint(c.Out, c.text(question)+" "+c.dim("canceled")+"\r\n")
		return 0, ErrCanceled
	}
	fmt.Fprint(c.Out, c.text(question)+" "+c.bold(c.text(options[m.cursor].Label))+"\r\n")
	return m.cursor, nil
}

// decide runs the arrow-key menu until the user chooses or cancels, calling
// redraw after every change of highlight. It is the loop chooseRaw runs, kept
// free of terminal setup so tests drive exactly the shipped behaviour.
func decide(keys *keyReader, m model, redraw func(model)) (model, error) {
	for {
		k, err := keys.next()
		if err != nil {
			return m, err
		}
		before := m.cursor
		m = m.step(k)
		if m.chosen || m.canceled {
			return m, nil
		}
		if m.cursor != before && redraw != nil {
			redraw(m)
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
	lines := []string{oneLine(c.text(question))}
	for i, opt := range options {
		marker, label := "  ", oneLine(c.text(opt.Label))
		if i == cursor {
			marker = "> "
			if !c.NoColor {
				marker = "\x1b[36m›\x1b[0m "
			}
			label = c.bold(label)
		}
		line := marker + label
		if opt.Hint != "" {
			line += "  " + c.dim(oneLine(c.text(opt.Hint)))
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
	n := displayWidth(ansiSequence.ReplaceAllString(line, ""))
	if width <= 0 || n <= width {
		return 1
	}
	return (n + width - 1) / width
}

// displayWidth approximates terminal columns: East Asian wide characters and
// emoji take two, combining marks none.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x0300 && r <= 0x036f, r == 0x200d, r >= 0xfe00 && r <= 0xfe0f:
		case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
			r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
			r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1f64f, r >= 0x1f900 && r <= 0x1f9ff,
			r >= 0x20000 && r <= 0x3fffd:
			w += 2
		default:
			w++
		}
	}
	return w
}

// oneLine keeps menu text on a single row so the redraw count stays exact:
// line breaks and tabs become spaces and other control characters are dropped.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
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
)

// model is the pure selection state. Only Enter chooses; only the up and down
// arrows move the highlight, which stops at the ends instead of wrapping.
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

// next decodes one key. Only Enter, the cancel keys and the up and down arrows
// mean anything; every other byte is ignored.
func (r *keyReader) next() (key, error) {
	b, err := r.byte()
	if err != nil {
		return keyNone, err
	}
	switch b {
	case '\r', '\n':
		return keyEnter, nil
	case 3, 4, 'q', 'Q': // Ctrl-C, Ctrl-D
		return keyCancel, nil
	case 0x1b:
		return r.escape()
	}
	return keyNone, nil
}

// escape consumes an escape sequence so none of its bytes are read as separate
// keys: CSI (ESC [ parameters intermediates final), SS3 (ESC O x), or ESC
// followed by an unrelated byte, which is put back. A byte that cannot belong
// to a CSI sequence ends it and is put back too, so Enter or Ctrl-C after a
// malformed sequence still counts. Only plain up and down arrows move.
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
		if third < 0x20 {
			r.unread(third)
			return keyNone, nil
		}
		return arrow(third, ""), nil
	case '[':
		var params []byte
		for {
			b, err := r.byte()
			if err != nil {
				return keyNone, err
			}
			switch {
			case b >= 0x40 && b <= 0x7e: // final byte
				if b == '~' && string(params) == "200" {
					return keyNone, r.skipPaste()
				}
				return arrow(b, string(params)), nil
			case b >= 0x20 && b <= 0x3f && len(params) < 16: // parameters, intermediates
				params = append(params, b)
			default:
				r.unread(b)
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
			// A line without Enter (for example "y" then Ctrl-D) is not an
			// answer; end of input cancels.
			return "", err
		}
	}
}
