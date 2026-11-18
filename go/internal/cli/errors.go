// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package cli is the Carbyne Stack command line application (Java io.carbynestack.cli): the "cs"
// program with its configure and login commands and the amphora, castor, ephemeral and thymus clients.
package cli

import (
	"strings"

	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// CliError is CsCliException: the command line could not be parsed or the command could not be set
// up. Help is the usage of the client.
type CliError struct {
	Msg   string
	Cause error
	Help  string
}

func (e *CliError) Error() string { return e.Msg }

// Message is the exception message.
func (e *CliError) Message() string { return e.Msg }

func (e *CliError) Unwrap() error { return e.Cause }

// RunnerError is CsCliRunnerException: the command failed while running.
type RunnerError struct {
	Msg   string
	Cause error
}

func (e *RunnerError) Error() string { return e.Msg }

// Message is the exception message.
func (e *RunnerError) Message() string { return e.Msg }

func (e *RunnerError) Unwrap() error { return e.Cause }

func runnerErr(msg string, cause error) error { return &RunnerError{Msg: msg, Cause: cause} }

// UUIDConverter is UUIDTypeConverter: java.util.UUID.fromString, failing with "Invalid UUID <value>.".
func parseUUIDArg(value string) (string, error) {
	u, err := javautil.ParseUUID(value)
	if err != nil {
		return "", &jc.ParameterError{Msg: props.Format(props.CsCli, "uuid.conversion.exception", value)}
	}
	return u.String(), nil
}

// parsePathArg is PathTypeConverter (Paths.get): a path must not contain NUL.
func parsePathArg(value string) (string, error) {
	if strings.ContainsRune(value, 0) {
		return "", &jc.ParameterError{Msg: props.Format(props.CsCli, "path.conversion.exception", value)}
	}
	return value, nil
}

// hiddenHelp is the hidden --help option every client command has (CsClientCliCommandConfig).
func hiddenHelp(target *bool) *jc.Option {
	return &jc.Option{Names: []string{"--help"}, Description: props.Get(props.CsCli, "option.help-description"),
		Help: true, Hidden: true, Order: -1, Kind: jc.Bool, Arity: -1, Flip: func() { *target = !*target }}
}
