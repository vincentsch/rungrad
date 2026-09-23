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

func TestLineModeRejectsPartialAnswersAndPastedText(t *testing.T) {
	// A partial label or an unrelated pasted line must ask again, and end of
	// input then cancels instead of choosing anything.
	for _, input := range []string{"l\n", "keep\n", "unspar project list\n", "ye\n"} {
		_, err := Chooser{In: strings.NewReader(input), Out: &bytes.Buffer{}}.Choose("Q?", loginOptions, 1)
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("input %q: want ErrCanceled after re-asking, got %v", input, err)
		}
	}
}

func TestLineModeEOFCancels(t *testing.T) {
	_, err := Chooser{In: strings.NewReader(""), Out: &bytes.Buffer{}}.Choose("Q?", []Option{{Label: "a"}}, 0)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
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

// run feeds keys to the raw-mode state machine exactly as chooseRaw does.
func run(t *testing.T, input string, options []Option, def int) model {
	t.Helper()
	r := newKeyReader(strings.NewReader(input))
	m := model{cursor: def, count: len(options)}
	for !m.chosen && !m.canceled {
		k, raw, err := r.next()
		if err != nil {
			return m
		}
		if k == keyNone {
			if idx, ok := aliasKey(raw, options); ok {
				k = keyDigit1 + key(idx)
			}
		}
		m = m.step(k)
	}
	return m
}

var yesNo = []Option{{Label: "Yes", Aliases: []string{"y"}}, {Label: "No", Aliases: []string{"n"}}}

func TestRawModeOnlyEnterChooses(t *testing.T) {
	for _, input := range []string{"1", "y", "k", "\x1b[A", "2y1"} {
		if m := run(t, input, yesNo, 1); m.chosen {
			t.Fatalf("input %q chose without Enter: %+v", input, m)
		}
	}
	if m := run(t, "y\r", yesNo, 1); !m.chosen || m.cursor != 0 {
		t.Fatalf("y then Enter should choose Yes: %+v", m)
	}
	if m := run(t, "\r", yesNo, 1); !m.chosen || m.cursor != 1 {
		t.Fatalf("Enter alone should keep the safe default: %+v", m)
	}
}

func TestRawModeHighlightDoesNotWrap(t *testing.T) {
	if m := run(t, "j\r", yesNo, 1); m.cursor != 1 {
		t.Fatalf("j at the last option must stay there, got cursor %d", m.cursor)
	}
	if m := run(t, "kk\r", yesNo, 1); m.cursor != 0 {
		t.Fatalf("k past the top must stop at 0, got %d", m.cursor)
	}
}

func TestRawModeSwallowsWholeEscapeSequences(t *testing.T) {
	sequences := map[string]string{
		"F10 xterm":        "\x1b[21~",
		"F1 rxvt":          "\x1b[11~",
		"F5":               "\x1b[15~",
		"shift down":       "\x1b[1;2B",
		"ctrl up":          "\x1b[1;5A",
		"bracketed paste":  "\x1b[200~yes\r\x1b[A\r\x1b[201~",
		"page up":          "\x1b[5~",
		"ss3 F1":           "\x1bOP",
		"delete":           "\x1b[3~",
		"lone esc then n":  "\x1bn",
		"lone esc then up": "\x1b\x1b[A",
	}
	for name, seq := range sequences {
		m := run(t, seq+"\r", yesNo, 1)
		want := 1
		switch name {
		case "lone esc then up":
			want = 0
		}
		if !m.chosen || m.cursor != want {
			t.Fatalf("%s: highlight %d chosen %v, want %d", name, m.cursor, m.chosen, want)
		}
	}
}

func TestRawModeCancelKeys(t *testing.T) {
	for _, input := range []string{"q", "\x03", "\x04"} {
		if m := run(t, input, yesNo, 1); !m.canceled {
			t.Fatalf("%q should cancel", input)
		}
	}
}

func TestWrappedRowsCountsLongLines(t *testing.T) {
	if got := wrappedRows(strings.Repeat("x", 81), 80); got != 2 {
		t.Fatalf("81 chars at 80 cols = %d rows, want 2", got)
	}
	if got := wrappedRows("\x1b[1m"+strings.Repeat("x", 80)+"\x1b[0m", 80); got != 1 {
		t.Fatalf("escape codes must not count toward width, got %d", got)
	}
}
