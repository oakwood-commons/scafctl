// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package input provides interactive user input functionality for the terminal.
package input

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"golang.org/x/term"
)

type Input struct {
	ioStreams     *terminal.IOStreams
	cliParams     *settings.Run
	testMode      bool
	testResponses []string
	testIndex     int
}

func New(ioStreams *terminal.IOStreams, cliParams *settings.Run) *Input {
	return &Input{
		ioStreams: ioStreams,
		cliParams: cliParams,
		testMode:  false,
	}
}

func (i *Input) Confirm(opts *ConfirmOptions) (bool, error) {
	if opts == nil {
		opts = NewConfirmOptions()
	}
	if opts.SkipCondition {
		return opts.Default, nil
	}
	if i.cliParams.IsQuiet {
		return opts.Default, nil
	}
	if !i.testMode && !i.isTTY() {
		return false, fmt.Errorf("cannot prompt for confirmation: stdin is not a terminal")
	}
	promptSuffix := " (y/N): "
	if opts.Default {
		promptSuffix = " (Y/n): "
	}
	fullPrompt := opts.Prompt + promptSuffix
	fmt.Fprint(i.ioStreams.Out, fullPrompt)
	response, err := i.readLine()
	if err != nil {
		return opts.Default, fmt.Errorf("failed to read confirmation: %w", err)
	}
	response = strings.TrimSpace(strings.ToLower(response))
	if response == "" {
		return opts.Default, nil
	}
	switch response {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return opts.Default, nil
	}
}

func (i *Input) ReadPassword(opts *PasswordOptions) (string, error) {
	if opts == nil {
		opts = NewPasswordOptions()
	}
	if !i.testMode && !i.isTTY() {
		return "", fmt.Errorf("cannot read password: stdin is not a terminal")
	}
	fmt.Fprint(i.ioStreams.ErrOut, opts.Prompt)
	password, err := i.readPassword()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Fprintln(i.ioStreams.ErrOut)
	if err := i.validatePassword(password, opts); err != nil {
		return "", err
	}
	if opts.RequireConfirmation {
		fmt.Fprint(i.ioStreams.ErrOut, opts.ConfirmPrompt)
		confirmPassword, err := i.readPassword()
		if err != nil {
			return "", fmt.Errorf("failed to read confirmation password: %w", err)
		}
		fmt.Fprintln(i.ioStreams.ErrOut)
		if password != confirmPassword {
			return "", fmt.Errorf("passwords do not match")
		}
	}
	return password, nil
}

func (i *Input) ReadLine(opts *LineOptions) (string, error) {
	if opts == nil {
		opts = NewLineOptions()
	}
	if !i.testMode && !i.isTTY() {
		return "", fmt.Errorf("cannot read input: stdin is not a terminal")
	}
	fullPrompt := opts.Prompt
	if opts.Default != "" {
		fullPrompt = fmt.Sprintf("%s [%s]: ", strings.TrimSuffix(opts.Prompt, ": "), opts.Default)
	}
	fmt.Fprint(i.ioStreams.Out, fullPrompt)
	line, err := i.readLine()
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		if opts.Default != "" {
			line = opts.Default
		} else if !opts.AllowEmpty {
			return "", fmt.Errorf("input cannot be empty")
		}
	}
	if opts.Validator != nil {
		if err := opts.Validator(line); err != nil {
			return "", err
		}
	}
	return line, nil
}

func (i *Input) isTTY() bool {
	if f, ok := i.ioStreams.In.(*os.File); ok {
		return term.IsTerminal(int(f.Fd())) //nolint:gosec // G115: Fd() fits in int on all supported platforms
	}
	return false
}

func (i *Input) readLine() (string, error) {
	if i.testMode {
		if i.testIndex >= len(i.testResponses) {
			return "", fmt.Errorf("no more test responses available")
		}
		response := i.testResponses[i.testIndex]
		i.testIndex++
		return response, nil
	}
	reader := bufio.NewReader(i.ioStreams.In)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func (i *Input) readPassword() (string, error) {
	if i.testMode {
		if i.testIndex >= len(i.testResponses) {
			return "", fmt.Errorf("no more test responses available")
		}
		response := i.testResponses[i.testIndex]
		i.testIndex++
		return response, nil
	}
	f, ok := i.ioStreams.In.(*os.File)
	if !ok {
		return "", fmt.Errorf("stdin is not a file")
	}
	passwordBytes, err := term.ReadPassword(int(f.Fd())) //nolint:gosec // G115: Fd() fits in int on all supported platforms
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

func (i *Input) validatePassword(password string, opts *PasswordOptions) error {
	if password == "" && !opts.AllowEmpty {
		return fmt.Errorf("password cannot be empty")
	}
	if opts.MinLength > 0 && len(password) < opts.MinLength {
		return fmt.Errorf("password must be at least %d characters", opts.MinLength)
	}
	return nil
}
