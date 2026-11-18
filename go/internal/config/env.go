// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Samrat7745/cli/go/internal/logx"
	"github.com/Samrat7745/cli/go/internal/props"
)

// Env is the process context the Java CLI keeps in static state and system properties: standard
// streams, the logger, environment variables, the configuration and token file locations
// (Configuration.configFilePath, VcpTokenStore.customDefaultLocation) and the cached configuration
// (Configuration.singletonInstance). Tests create their own Env.
type Env struct {
	In     *bufio.Reader
	Out    io.Writer
	Log    *logx.Logger
	Getenv func(string) (string, bool)
	Now    func() time.Time
	// Home and WorkDir are the user.home and user.dir system properties.
	Home    string
	WorkDir string
	// ConfigPath is Configuration.configFilePath ("" = ~/.cs/config, resolved on first use).
	ConfigPath string
	// TokenPath is VcpTokenStore's custom default location ("" = ~/.cs/access-tokens.json).
	TokenPath string

	config *Configuration
}

// NewEnv returns the environment of the running process.
func NewEnv() *Env {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	return &Env{
		In:      bufio.NewReader(os.Stdin),
		Out:     os.Stdout,
		Log:     logx.New(os.Stdout),
		Getenv:  os.LookupEnv,
		Now:     time.Now,
		Home:    home,
		WorkDir: wd,
	}
}

// Printf writes to standard output (System.out.printf/print).
func (e *Env) Printf(format string, a ...any) {
	_, _ = fmt.Fprintf(e.Out, format, a...)
}

// Println writes s and a line separator (System.out.println).
func (e *Env) Println(s string) { _, _ = io.WriteString(e.Out, s+"\n") }

func (e *Env) env(key string) (string, bool) {
	if e.Getenv == nil {
		return os.LookupEnv(key)
	}
	return e.Getenv(key)
}

// ReadLine reads one line from standard input like Scanner.nextLine; io.EOF if there is none.
func (e *Env) ReadLine() (string, error) {
	line, err := e.In.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// ReadAll reads standard input to the end (ByteStreams.toByteArray(System.in)).
func (e *Env) ReadAll() (string, error) {
	b, err := io.ReadAll(e.In)
	return string(b), err
}

// ReadOrDefault is ConsoleReader.readOrDefault: a line of input, or def if it is empty or cannot be read.
func (e *Env) ReadOrDefault(def string) string {
	in, err := e.ReadLine()
	if err != nil {
		e.Log.Error(props.Get(props.Configuration, "read-input.failed"), err)
	}
	if in == "" {
		return def
	}
	return in
}
