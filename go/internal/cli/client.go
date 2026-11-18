// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/login"
	"github.com/Samrat7745/cli/go/internal/props"
)

// Runner runs a parsed command (CsClientCliCommandRunner.run).
type Runner interface {
	Run() error
}

// ClientCommand is a command of a client (CsClientCliCommand: its configuration and runner).
type ClientCommand struct {
	Name        string
	Description string
	Options     []*jc.Option
	Main        *jc.Main
	// NewRunner is the runner's constructor; errors other than login errors make the command fail
	// with "<program> failed to execute command." (Java wraps them in InvocationTargetException).
	NewRunner func() (Runner, error)
}

// ClientCli is CsClientCli: the parser and dispatcher of one client's commands.
type ClientCli struct {
	Env      *config.Env
	Name     string
	Args     []string
	Debug    bool
	Commands []*ClientCommand

	opts mainOptions
	jc   *jc.JC
}

// ProgramName is "cs <client>".
func (c *ClientCli) ProgramName() string {
	return fmt.Sprintf("%s %s", config.ApplicationTitle, c.Name)
}

func (c *ClientCli) parser() *jc.JC {
	root := &jc.JC{ProgramName: c.ProgramName(), Options: c.opts.list(), ColumnSize: jc.MaxColumnSize}
	for _, cmd := range c.Commands {
		root.Commands = append(root.Commands, &jc.Command{Name: cmd.Name, Description: cmd.Description,
			JC: &jc.JC{ProgramName: cmd.Name, Options: cmd.Options, Main: cmd.Main}})
	}
	return root
}

// Parse parses the client arguments and runs the command.
func (c *ClientCli) Parse() error {
	c.jc = c.parser()
	if err := c.jc.Parse(c.Args...); err != nil {
		var pe *jc.ParameterError
		if errors.As(err, &pe) {
			return &CliError{Msg: pe.Msg + "\n\n", Cause: pe, Help: pe.JC.Usage()}
		}
		return err
	}
	if c.opts.help {
		c.Env.Println(props.UnescapeJava(c.jc.Usage()))
		return nil
	}
	name := c.jc.ParsedCommand()
	if name == "" {
		// Java attaches an empty Throwable as cause ("Caused by: java.lang.Throwable").
		return &CliError{Msg: props.Get(props.CsCli, "error.no-command"), Cause: errors.New(""), Help: c.jc.Usage()}
	}
	for _, cmd := range c.Commands {
		if cmd.Name != name {
			continue
		}
		r, err := cmd.NewRunner()
		if err != nil {
			var le *login.Error
			if errors.As(err, &le) {
				return le
			}
			return &CliError{Msg: props.Format(props.CsCli, "error.failed-execute-command", c.ProgramName()),
				Cause: err, Help: c.jc.Usage()}
		}
		return r.Run()
	}
	return nil
}

// tokensByBaseURL loads (and refreshes) the token store if it exists; nil if there is none.
func tokensByBaseURL(e *config.Env) (map[string]login.Token, error) {
	if !login.Exists(e) {
		return nil, nil
	}
	s, aerr := login.Load(e, true)
	if aerr != nil {
		return nil, &login.Error{Err: aerr}
	}
	out := map[string]login.Token{}
	for _, t := range s.Tokens {
		out[t.VcpBaseURL] = t
	}
	return out, nil
}

// providerFor returns the provider whose base URL is vcpBaseURL (the Java runners' token lookup).
func providerFor(cfg *config.Configuration, vcpBaseURL string) (*config.VcpConfiguration, error) {
	for _, p := range cfg.Providers {
		if p.BaseURL == vcpBaseURL {
			return p, nil
		}
	}
	return nil, errors.New("java.lang.IllegalStateException: mismatch between configuration and token store")
}

// normalizeServiceURL is how the Go service clients address a service (http unless https, no trailing
// slash), used to look up a service's token.
func normalizeServiceURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	if u.Scheme != "https" {
		u.Scheme = "http"
	}
	return strings.TrimSuffix(u.String(), "/")
}
