package prompt

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEnterAcceptsTheDefault(t *testing.T) {
	var out strings.Builder
	p := New(strings.NewReader("\n"), &out, false, true)

	got, err := p.Ask("package name:", "urls")
	if err != nil {
		t.Fatal(err)
	}
	if got != "urls" {
		t.Errorf("got %q, want the default", got)
	}
	if !strings.Contains(out.String(), "(urls)") {
		t.Errorf("the default should be shown in the prompt, got %q", out.String())
	}
}

func TestAnswerOverridesTheDefault(t *testing.T) {
	p := New(strings.NewReader("@acme/urls\n"), &strings.Builder{}, false, true)
	got, _ := p.Ask("package name:", "urls")
	if got != "@acme/urls" {
		t.Errorf("got %q, want the typed answer", got)
	}
}

func TestReAsksUntilValid(t *testing.T) {
	var out strings.Builder
	// Two rejected answers, then a good one.
	p := New(strings.NewReader("Bad Name\nstill bad\ngood-name\n"), &out, false, true)

	validate := func(s string) error {
		if strings.Contains(s, " ") || strings.ToLower(s) != s {
			return errors.New("must be lowercase with no spaces")
		}
		return nil
	}

	got, err := p.AskValid("package name:", "", validate)
	if err != nil {
		t.Fatal(err)
	}
	if got != "good-name" {
		t.Errorf("got %q, want the first valid answer", got)
	}
	if n := strings.Count(out.String(), "must be lowercase"); n != 2 {
		t.Errorf("expected 2 validation messages, got %d in %q", n, out.String())
	}
}

func TestYesAcceptsEveryDefaultWithoutReading(t *testing.T) {
	// An empty reader proves nothing was read from stdin.
	p := New(strings.NewReader(""), &strings.Builder{}, true, false)
	got, err := p.Ask("version:", "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.1.0" {
		t.Errorf("got %q, want the default", got)
	}
}

func TestYesStillRejectsAnInvalidDefault(t *testing.T) {
	p := New(strings.NewReader(""), &strings.Builder{}, true, false)
	_, err := p.AskValid("package name:", "Not Valid", func(string) error {
		return errors.New("bad")
	})
	if err == nil {
		t.Error("-y must not write a config the validator rejects")
	}
}

func TestNonInteractiveWithoutYesFailsRatherThanHanging(t *testing.T) {
	// This is the CI case: prompting a pipe would block forever.
	p := New(strings.NewReader(""), &strings.Builder{}, false, false)
	_, err := p.Ask("package name:", "urls")
	if !errors.Is(err, ErrNotInteractive) {
		t.Errorf("got %v, want ErrNotInteractive", err)
	}
}

func TestEOFAcceptsTheDefault(t *testing.T) {
	// Input that ends part-way through must not loop forever.
	p := New(strings.NewReader("first\n"), &strings.Builder{}, false, true)
	if _, err := p.Ask("one:", "a"); err != nil {
		t.Fatal(err)
	}
	got, err := p.Ask("two:", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "fallback" {
		t.Errorf("got %q, want the default after EOF", got)
	}
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		input string
		def   bool
		want  bool
	}{
		{"\n", true, true},
		{"\n", false, false},
		{"y\n", false, true},
		{"yes\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"YES\n", false, true},
		{"nonsense\n", true, true},
	}
	for _, c := range cases {
		p := New(strings.NewReader(c.input), &strings.Builder{}, false, true)
		got, err := p.Confirm("Is this OK?", c.def)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("Confirm(%q, default %v) = %v, want %v", c.input, c.def, got, c.want)
		}
	}
}

func TestPromptsAreOrderedAndLabelled(t *testing.T) {
	var out strings.Builder
	p := New(strings.NewReader("\n\n\n"), &out, false, true)
	for _, q := range []string{"package name:", "version:", "license:"} {
		if _, err := p.Ask(q, "x"); err != nil {
			t.Fatal(err)
		}
	}
	text := out.String()
	last := -1
	for _, q := range []string{"package name:", "version:", "license:"} {
		i := strings.Index(text, q)
		if i < 0 {
			t.Fatalf("%q was never asked; got %q", q, text)
		}
		if i < last {
			t.Errorf("%q asked out of order", q)
		}
		last = i
	}
	fmt.Fprint(&out, "")
}
