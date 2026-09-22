package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadPasswordLineTrimsExactlyOneLineEnding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"lf", "hunter2\n", "hunter2"},
		{"crlf", "hunter2\r\n", "hunter2"},
		{"no trailing newline (EOF)", "hunter2", "hunter2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readPasswordLine(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("readPasswordLine(%q): %v", tc.input, err)
			}
			if string(got) != tc.want {
				t.Errorf("readPasswordLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestReadPasswordLineReadsOnlyTheFirstLine(t *testing.T) {
	got, err := readPasswordLine(strings.NewReader("hunter2\nsomething-else\n"))
	if err != nil {
		t.Fatalf("readPasswordLine: %v", err)
	}
	if string(got) != "hunter2" {
		t.Errorf("readPasswordLine read %q, want just the first line %q", got, "hunter2")
	}
}

func TestReadPasswordLineRefusesEmpty(t *testing.T) {
	for _, input := range []string{"", "\n", "\r\n"} {
		if _, err := readPasswordLine(strings.NewReader(input)); err == nil {
			t.Errorf("readPasswordLine(%q) succeeded, want an error for an empty password", input)
		}
	}
}

func TestReadPasswordDispatchesOnIsTerminal(t *testing.T) {
	r, w, err := osPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("hunter2\n"); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	_ = w.Close()

	got, err := readPassword(r, &bytes.Buffer{}, func(uintptr) bool { return false })
	if err != nil {
		t.Fatalf("readPassword: %v", err)
	}
	if string(got) != "hunter2" {
		t.Errorf("readPassword (non-terminal) = %q, want %q", got, "hunter2")
	}
}
