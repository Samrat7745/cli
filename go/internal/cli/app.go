// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/login"
	"github.com/Samrat7745/cli/go/internal/logx"
	"github.com/Samrat7745/cli/go/internal/props"
)

// Exit codes of the Java CLI.
const (
	ExitOK            = 0
	ExitParameter     = 1 // ParameterException, and uncaught runtime exceptions
	ExitCli           = 2 // CsCliException
	ExitRunner        = 3 // CsCliRunnerException
	ExitConfiguration = 4 // CsCliConfigurationException
	ExitLogin         = 5 // CsCliLoginException
)

// Client is a registered service client (CsClient): its name, description and CLI.
type Client struct {
	Name        string
	Description string
	// New creates the client CLI for the arguments after the client name.
	New func(e *config.Env, args []string, debug bool) *ClientCli
}

// options of CsCliConfig, shared by the application and every client CLI.
type mainOptions struct {
	help, debug           bool
	configFile, tokenFile string
}

func (m *mainOptions) list() []*jc.Option {
	cs := func(k string) string { return props.Get(props.CsCli, k) }
	return []*jc.Option{
		{Names: []string{"--help"}, Description: cs("option.help-description"), Help: true, Order: -1, Kind: jc.Bool,
			Arity: -1, Flip: func() { m.help = !m.help }},
		{Names: []string{"--debug"}, Description: cs("option.debug-description"), Help: true, Order: -1, Kind: jc.Bool,
			Arity: -1, Flip: func() { m.debug = !m.debug }},
		{Names: []string{"--config-file"}, Description: cs("option.config-file"), Order: -1, Kind: jc.Value, Arity: -1,
			Set: func(_, v string) (err error) { m.configFile, err = parsePathArg(v); return }},
		{Names: []string{"--access-token-file"}, Description: cs("option.access-token-file"), Order: -1, Kind: jc.Value,
			Arity: -1, Set: func(_, v string) (err error) { m.tokenFile, err = parsePathArg(v); return }},
	}
}

// App is the "cs" application (CsCliApplication).
type App struct {
	Env       *config.Env
	Configure *config.Command
	Login     *login.Command
	Clients   []Client

	opts mainOptions
}

// NewApp returns the application with the four service clients.
func NewApp(e *config.Env) *App {
	a := &App{Env: e, Configure: &config.Command{}, Login: login.NewCommand()}
	a.Clients = []Client{AmphoraClient(nil), CastorClient(nil, nil), EphemeralClient(nil), ThymusClient(nil)}
	return a
}

func (a *App) parser() (*jc.JC, map[string]*[]string) {
	root := &jc.JC{ProgramName: config.ApplicationTitle, Options: a.opts.list()}
	root.Commands = append(root.Commands,
		&jc.Command{Name: config.CommandName, Description: a.Configure.Description(),
			JC: &jc.JC{ProgramName: config.CommandName, Options: a.Configure.Options()}},
		&jc.Command{Name: login.CommandName, Description: a.Login.Description(),
			JC: &jc.JC{ProgramName: login.CommandName, Options: a.Login.Options()}})
	args := map[string]*[]string{}
	for _, c := range a.Clients {
		list := &[]string{}
		args[c.Name] = list
		root.Commands = append(root.Commands, &jc.Command{Name: c.Name, Description: c.Description,
			JC: &jc.JC{ProgramName: c.Name, Main: &jc.Main{Description: "<args>", Arity: -1, List: true,
				Set: func(v string) error { *list = append(*list, v); return nil }}}})
	}
	return root, args
}

// Run parses args and runs the command (CsCliApplication.run).
func (a *App) Run(args ...string) error {
	root, clientArgs := a.parser()
	if err := root.Parse(args...); err != nil {
		return err
	}
	root.ColumnSize = jc.MaxColumnSize
	cmd := root.ParsedCommand()
	if a.opts.help {
		a.Env.Println(props.UnescapeJava(root.Usage()))
		return nil
	}
	if a.opts.debug {
		a.Env.Log.SetLevel(logx.Debug)
	}
	if cmd == "" {
		return &jc.ParameterError{Msg: props.Get(props.CsCli, "error.no-command"), JC: root}
	}
	a.Env.ConfigPath = ""
	if a.opts.configFile != "" {
		p := a.opts.configFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.Env.WorkDir, p)
		}
		a.Env.ConfigPath = p
	}
	if a.opts.tokenFile != "" {
		a.Env.TokenPath = a.opts.tokenFile
	}
	switch cmd {
	case config.CommandName:
		return a.Configure.Configure(a.Env)
	case login.CommandName:
		return a.Login.Login(a.Env)
	}
	for _, c := range a.Clients {
		if c.Name == cmd {
			return c.New(a.Env, *clientArgs[c.Name], a.opts.debug).Parse()
		}
	}
	return nil
}

// Main runs the application and returns the process exit status, logging failures like the Java main.
func (a *App) Main(stderr io.Writer, args ...string) int {
	err := a.Run(args...)
	if err == nil {
		return ExitOK
	}
	log := a.Env.Log
	// Like the Java main, the error is classified by the thrown type, not by its causes.
	switch x := err.(type) {
	case *jc.ParameterError:
		log.Error(fmt.Sprintf("%s\n\n", x.Msg), nil)
		if x.JC != nil {
			a.Env.Println(x.JC.Usage())
		}
		return ExitParameter
	case *CliError:
		log.Error("Unexpected error occurred", x)
		log.Info(x.Help)
		return ExitCli
	case *RunnerError:
		log.Error(props.Format(props.CsCli, "error.command-execution", x.Msg), x)
		return ExitRunner
	case *config.Error:
		log.Error(x.Msg, x)
		return ExitConfiguration
	case *login.Error:
		log.Error(x.Error(), x)
		return ExitLogin
	}
	// An exception the Java main does not catch: the JVM reports it on stderr and exits with 1.
	fmt.Fprintf(stderr, "Exception in thread \"main\" %s", logx.Throwable(err))
	return ExitParameter
}

// Main is the program entry point.
func Main() int {
	return NewApp(config.NewEnv()).Main(os.Stderr, os.Args[1:]...)
}
