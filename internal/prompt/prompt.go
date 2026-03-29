// Package prompt implements the question-and-answer flow used by
// `gowasm init`, modelled on `npm init`.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNotInteractive is returned when a question needs an answer but there is
// nobody to ask. Failing here is deliberate: a prompt written to a pipe would
// otherwise hang a CI job forever.
var ErrNotInteractive = errors.New("stdin is not a terminal; re-run with -y to accept the defaults")

// Prompter asks questions on a terminal.
type Prompter struct {
	out         io.Writer
	scanner     *bufio.Scanner
	yes         bool
	interactive bool
}

// New builds a Prompter. When yes is true every question resolves to its
// default without being asked.
func New(in io.Reader, out io.Writer, yes, interactive bool) *Prompter {
	return &Prompter{
		out:         out,
		scanner:     bufio.NewScanner(in),
		yes:         yes,
		interactive: interactive,
	}
}

// Ask presents a question with a default, returning the default on empty input.
func (p *Prompter) Ask(label, def string) (string, error) {
	return p.AskValid(label, def, nil)
}

// AskValid is Ask with validation, re-asking until the answer passes. A default
// that fails validation is still re-asked, so -y cannot write a broken config.
func (p *Prompter) AskValid(label, def string, validate func(string) error) (string, error) {
	if p.yes || !p.interactive {
		if validate != nil {
			if err := validate(def); err != nil {
				if !p.interactive && !p.yes {
					return "", ErrNotInteractive
				}
				return "", fmt.Errorf("%s: %w", strings.TrimSuffix(label, ":"), err)
			}
		}
		if p.yes {
			return def, nil
		}
		return "", ErrNotInteractive
	}

	for {
		fmt.Fprintf(p.out, "%-18s", label)
		if def != "" {
			fmt.Fprintf(p.out, "(%s) ", def)
		}

		if !p.scanner.Scan() {
			if err := p.scanner.Err(); err != nil {
				return "", err
			}
			// EOF part-way through: treat it as accepting the default rather
			// than looping forever on a closed stream.
			fmt.Fprintln(p.out)
			return def, nil
		}

		answer := strings.TrimSpace(p.scanner.Text())
		if answer == "" {
			answer = def
		}
		if validate == nil {
			return answer, nil
		}
		if err := validate(answer); err != nil {
			fmt.Fprintf(p.out, "  %v\n", err)
			continue
		}
		return answer, nil
	}
}

// Confirm asks a yes/no question.
func (p *Prompter) Confirm(label string, def bool) (bool, error) {
	d := "yes"
	if !def {
		d = "no"
	}
	answer, err := p.Ask(label, d)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "true":
		return true, nil
	case "n", "no", "false":
		return false, nil
	default:
		return def, nil
	}
}

// Printf writes explanatory text between questions.
func (p *Prompter) Printf(format string, a ...any) {
	fmt.Fprintf(p.out, format, a...)
}
