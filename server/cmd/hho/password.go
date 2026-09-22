package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/auth"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
)

func readPassword(stdin *os.File, prompt io.Writer, isTerminal func(fd uintptr) bool) ([]byte, error) {
	fd := stdin.Fd()
	if isTerminal(fd) {
		return readPasswordFromTTY(fd, prompt)
	}
	return readPasswordLine(stdin)
}

func readPasswordFromTTY(fd uintptr, prompt io.Writer) ([]byte, error) {
	if _, err := fmt.Fprint(prompt, "New password: "); err != nil {
		return nil, fmt.Errorf("write password prompt: %w", err)
	}
	first, err := term.ReadPassword(int(fd))
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		return nil, fmt.Errorf("read password: %w", err)
	}

	if _, err := fmt.Fprint(prompt, "Confirm new password: "); err != nil {
		auth.Zero(first)
		return nil, fmt.Errorf("write password confirmation prompt: %w", err)
	}
	second, err := term.ReadPassword(int(fd))
	_, _ = fmt.Fprintln(prompt)
	if err != nil {
		auth.Zero(first)
		return nil, fmt.Errorf("read password confirmation: %w", err)
	}
	defer auth.Zero(second)

	if !bytes.Equal(first, second) {
		auth.Zero(first)
		return nil, errors.New("passwords do not match")
	}
	return first, nil
}

func readPasswordLine(r io.Reader) ([]byte, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read password from stdin: %w", err)
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return nil, errors.New("empty password on stdin")
	}
	return []byte(line), nil
}
