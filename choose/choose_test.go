package choose

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLineModeAcceptsNumbersLabelsAndBlankDefault(t *testing.T) {
	opts := []Option{{Label: "Log in again"}, {Label: "Keep the current login"}}
	cases := map[string]int{"1\n": 0, "2\n": 1, "\n": 1, "l\n": 0, "keep\n": 1, "9\nlog\n": 0}
	for input, want := range cases {
		var out bytes.Buffer
		got, err := Chooser{In: strings.NewReader(input), Out: &out}.Choose("Replace it?", opts, 1)
		if err != nil || got != want {
			t.Fatalf("input %q: got %d, %v; want %d", input, got, err, want)
		}
		if !strings.Contains(out.String(), "  1) Log in again") || !strings.Contains(out.String(), "Choose 1-2 [2]") {
			t.Fatalf("input %q rendered %q", input, out.String())
		}
	}
}

func TestLineModeEOFCancels(t *testing.T) {
	_, err := Chooser{In: strings.NewReader(""), Out: &bytes.Buffer{}}.Choose("Q?", []Option{{Label: "a"}}, 0)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("want ErrCanceled, got %v", err)
	}
}

func TestConfirmDefaultsToTheSafeAnswer(t *testing.T) {
	ok, err := Chooser{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}.Confirm("Delete?", "Delete it", "Keep it")
	if err != nil || ok {
		t.Fatalf("blank answer must choose the no option, got %v %v", ok, err)
	}
}

func TestKeyDecodingAndModel(t *testing.T) {
	// down, down (wraps), up, enter
	r := newKeyReader(strings.NewReader("\x1b[B\x1b[B\x1bOA\r"))
	m := model{cursor: 0, count: 2}
	for !m.chosen {
		k, err := r.next()
		if err != nil {
			t.Fatal(err)
		}
		m = m.step(k)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	m = model{count: 3}.step(keyDigit1 + 2)
	if !m.chosen || m.cursor != 2 {
		t.Fatalf("digit 3 should choose index 2: %+v", m)
	}
	if m := (model{count: 2}).step(keyDigit1 + 5); m.chosen {
		t.Fatal("out-of-range digit must be ignored")
	}
	if k, _ := newKeyReader(strings.NewReader("q")).next(); k != keyCancel {
		t.Fatal("q must cancel")
	}
	if k, _ := newKeyReader(strings.NewReader("\x03")).next(); k != keyCancel {
		t.Fatal("ctrl-c must cancel")
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

func TestConfirmAcceptsYesNoAliases(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "no\n": false, "keep\n": false, "replace\n": true} {
		ok, err := Chooser{In: strings.NewReader(input), Out: &bytes.Buffer{}}.Confirm("Q?", "Replace it", "Keep it")
		if err != nil || ok != want {
			t.Fatalf("input %q: got %v %v, want %v", input, ok, err, want)
		}
	}
	if idx, ok := aliasKey('n', []Option{{Label: "a", Aliases: []string{"y"}}, {Label: "b", Aliases: []string{"n"}}}); !ok || idx != 1 {
		t.Fatalf("single-key alias n = %d %v", idx, ok)
	}
}
