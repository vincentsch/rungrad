package choose

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

var loginOptions = []Option{{Label: "Log in again"}, {Label: "Keep the current login"}}

func TestLineModeAcceptsNumbersFullLabelsAndBlankDefault(t *testing.T) {
	cases := map[string]int{"1\n": 0, "2\n": 1, "\n": 1, "keep the current login\n": 1, "9\n1\n": 0}
	for input, want := range cases {
		var out bytes.Buffer
		got, err := Chooser{In: strings.NewReader(input), Out: &out}.Choose("Replace it?", loginOptions, 1)
		if err != nil || got != want {
			t.Fatalf("input %q: got %d, %v; want %d", input, got, err, want)
		}
		if !strings.Contains(out.String(), "  1) Log in again") || !strings.Contains(out.String(), "Choose 1-2 [2]") {
			t.Fatalf("input %q rendered %q", input, out.String())
		}
	}
}

func TestLineModeRejectsPartialAnswersPastedTextAndOddNumbers(t *testing.T) {
	// Anything that is not exactly an answer asks again; end of input then
	// cancels instead of choosing.
	for _, input := range []string{"l\n", "keep\n", "unspar project list\n", "ye\n", "01\n", "+1\n", "y"} {
		_, err := Chooser{In: strings.NewReader(input), Out: &bytes.Buffer{}}.Choose("Q?", loginOptions, 1)
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("input %q: want ErrCanceled, got %v", input, err)
		}
	}
}

func TestLineModeQCancels(t *testing.T) {
	_, err := Chooser{In: strings.NewReader("q\n1\n"), Out: &bytes.Buffer{}}.Choose("Q?", loginOptions, 1)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("q must cancel, got %v", err)
	}
}

func TestInvalidDefaultIsAnError(t *testing.T) {
	for _, def := range []int{-1, 2} {
		if _, err := (Chooser{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}).Choose("Q?", loginOptions, def); err == nil {
			t.Fatalf("default %d should be rejected", def)
		}
	}
}

func TestConfirmDefaultsToTheSafeAnswerAndAcceptsYesNo(t *testing.T) {
	for input, want := range map[string]bool{"\n": false, "y\n": true, "YES\n": true, "n\n": false, "no\n": false, "1\n": true, "Delete it\n": true} {
		ok, err := Chooser{In: strings.NewReader(input), Out: &bytes.Buffer{}}.Confirm("Delete?", "Delete it", "Keep it")
		if err != nil || ok != want {
			t.Fatalf("input %q: got %v %v, want %v", input, ok, err, want)
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestUnwritableQuestionIsNotAnswered(t *testing.T) {
	want := errors.New("stderr closed")
	var consumed bytes.Buffer
	in := io.TeeReader(strings.NewReader("1\n"), &consumed)
	_, err := Chooser{In: in, Out: failingWriter{want}}.Choose("Q?", []Option{{Label: "a"}}, 0)
	if !errors.Is(err, want) {
		t.Fatalf("want the write error, got %v", err)
	}
	if consumed.Len() != 0 {
		t.Fatalf("read an answer to a question that was never shown: %q", consumed.String())
	}
}

// menu drives decide, the exact loop chooseRaw runs, from Confirm's options
// with "No" preselected.
func menu(input string) (model, error) {
	return decide(newKeyReader(strings.NewReader(input)), model{cursor: 1, count: 2}, nil)
}

func TestMenuTypedOrPastedTextCanOnlyPickTheSafeDefault(t *testing.T) {
	lines := []string{
		"cd ~/work\r", "make\r", "ls -1\r", "git log -1\r", "docker ps\r", "history\r",
		"kubectl get pods\r", "yes\r", "y\r", "1\r", "jjkk\r", "unspar project list\n",
		strings.Repeat("kyk1", 5000) + "\r", // far beyond any typeahead drain
	}
	for _, input := range lines {
		m, err := menu(input)
		if err != nil || !m.chosen || m.cursor != 1 {
			name := input
			if len(name) > 40 {
				name = name[:40] + "..."
			}
			t.Fatalf("%q: chosen=%v cursor=%d err=%v; typed text must only ever pick the default", name, m.chosen, m.cursor, err)
		}
	}
}

func TestMenuArrowsThenEnterChoose(t *testing.T) {
	if m, _ := menu("\x1b[A\r"); !m.chosen || m.cursor != 0 {
		t.Fatalf("Up then Enter should choose the first answer: %+v", m)
	}
	if m, _ := menu("\x1bOA\r"); !m.chosen || m.cursor != 0 {
		t.Fatalf("SS3 Up then Enter should choose the first answer: %+v", m)
	}
	if m, _ := menu("\x1b[B\x1b[B\r"); m.cursor != 1 {
		t.Fatalf("Down at the last option must stay there: %+v", m)
	}
	if m, _ := menu("\x1b[A\x1b[A\x1b[A\r"); m.cursor != 0 {
		t.Fatalf("Up past the top must stop at 0: %+v", m)
	}
}

func TestMenuSwallowsWholeEscapeSequences(t *testing.T) {
	for name, seq := range map[string]string{
		"F10 xterm":             "\x1b[21~",
		"F1 rxvt":               "\x1b[11~",
		"shift F-key rxvt ($)":  "\x1b[23$",
		"shift up":              "\x1b[1;2A",
		"ctrl up":               "\x1b[1;5A",
		"bracketed paste":       "\x1b[200~\x1b[Ayes\r\x1b[201~",
		"paste with fake end":   "\x1b[200~x\x1b[201\x1b[A\r\x1b[201~",
		"page up":               "\x1b[5~",
		"ss3 F1":                "\x1bOP",
		"lone esc then y":       "\x1by",
		"malformed csi then up": "\x1b[1\x01",
	} {
		m, err := menu(seq + "\r")
		if err != nil || !m.chosen || m.cursor != 1 {
			t.Fatalf("%s: chosen=%v cursor=%d err=%v", name, m.chosen, m.cursor, err)
		}
	}
	// A malformed sequence must not swallow a following Enter or Ctrl-C.
	if m, _ := menu("\x1b[1\r"); !m.chosen {
		t.Fatal("Enter after a truncated CSI was swallowed")
	}
	if m, _ := menu("\x1b[23$\x03"); !m.canceled {
		t.Fatal("Ctrl-C after an rxvt shifted F-key was swallowed")
	}
}

func TestMenuCancelKeysAndEndOfInput(t *testing.T) {
	for _, input := range []string{"q", "\x03", "\x04"} {
		if m, _ := menu(input); !m.canceled {
			t.Fatalf("%q should cancel", input)
		}
	}
	if _, err := menu(""); !errors.Is(err, io.EOF) {
		t.Fatalf("end of input should surface as EOF for chooseRaw to cancel, got %v", err)
	}
}

func TestWrappedRowsCountsDisplayColumns(t *testing.T) {
	if got := wrappedRows(strings.Repeat("x", 81), 80); got != 2 {
		t.Fatalf("81 chars at 80 cols = %d rows, want 2", got)
	}
	if got := wrappedRows("\x1b[1m"+strings.Repeat("x", 80)+"\x1b[0m", 80); got != 1 {
		t.Fatalf("escape codes must not count toward width, got %d", got)
	}
	if got := wrappedRows(strings.Repeat("語", 41), 80); got != 2 {
		t.Fatalf("41 wide characters take 82 columns = 2 rows, got %d", got)
	}
	if got := oneLine("a\nb\tc\x07d"); got != "a b cd" {
		t.Fatalf("oneLine = %q", got)
	}
}

func TestMenuLoneEscFollowedByTypedArrowLettersDeclines(t *testing.T) {
	// Esc, a pause, then the user types "OAdd a line" and Enter: with nothing
	// waiting after the Esc it is a lone Esc, and the letters are ignored.
	r := newKeyReader(strings.NewReader("\x1bOAdd a line\r"))
	r.waiting = func() bool { return false }
	m, err := decide(r, model{cursor: 1, count: 2}, nil)
	if err != nil || !m.chosen || m.cursor != 1 {
		t.Fatalf("typed OA after a lone Esc confirmed: %+v %v", m, err)
	}
	r = newKeyReader(strings.NewReader("\x1b[A\r"))
	r.waiting = func() bool { return true }
	if m, _ := decide(r, model{cursor: 1, count: 2}, nil); m.cursor != 0 {
		t.Fatalf("a real arrow sequence must still move: %+v", m)
	}
}

func TestDisplayWidthCountsCommonEmojiAsWide(t *testing.T) {
	for _, r := range []string{"🚀", "✅", "⭐"} {
		if got := displayWidth(r); got != 2 {
			t.Fatalf("%s width = %d, want 2", r, got)
		}
	}
}
