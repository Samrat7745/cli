// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"

	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// Command is the "configure" command (ConfigurationCommand).
type Command struct {
	Help bool
	Show bool
	// Run replaces Configure when set (tests use it like the Java tests mock the command).
	Run func(*Env) error
}

// Options returns the command's parser options.
func (c *Command) Options() []*jc.Option {
	return []*jc.Option{
		{Names: []string{"--help"}, Description: msg("option.help-description"), Help: true, Order: -1, Kind: jc.Bool, Arity: -1,
			Flip: func() { c.Help = !c.Help }},
		{Names: []string{"show"}, Description: msg("option.show"), Order: -1, Kind: jc.Bool, Arity: -1,
			Default: "false", HasDefault: true, Flip: func() { c.Show = !c.Show }},
	}
}

// Description is the command description.
func (c *Command) Description() string { return msg("command-description") }

// Configure runs the command: help, show, or the interactive configuration.
func (c *Command) Configure(e *Env) error {
	if c.Run != nil {
		return c.Run(e)
	}
	if c.Help {
		e.Println(props.UnescapeJava(msg("detailed-help")))
		return nil
	}
	cfg := e.LoadFromFile()
	if c.Show {
		if cfg == nil {
			return &javautil.RuntimeError{Class: "java.lang.NullPointerException"}
		}
		e.Println(cfg.PrettyString())
		return nil
	}
	if cfg == nil {
		cfg = New(e)
		e.Log.Debugf(msg("read-config.log.fallback"))
	}
	err := cfg.Configure()
	if err == nil {
		err = c.Finalize(e, cfg)
	}
	if ce, ok := err.(*Error); ok {
		cause := ""
		if ce.Cause != nil {
			cause = ": " + ce.Cause.Error()
		}
		e.Log.Error(ce.Msg+cause, nil)
	}
	return err
}

// Finalize shows the configuration and stores it if the user confirms (finalizeConfiguration).
func (c *Command) Finalize(e *Env, cfg *Configuration) error {
	e.Println("\n**********************************")
	e.Println("Configuration:")
	e.Println(cfg.PrettyString())
	e.Println("\n**********************************")
	save, err := requestFinalizeStatus(e)
	if err != nil {
		return err
	}
	if save {
		e.Log.Debugf("Writing configuration to file: {}", cfg.String())
		return cfg.WriteToFile()
	}
	e.Log.Debugf("Discard configuration")
	return nil
}

func requestFinalizeStatus(e *Env) (bool, error) {
	e.Printf("\n%s", msg("configuration.finalize.request.complete", "yes"))
	in := ""
	for i := 0; i < 3; i++ {
		line, err := e.ReadLine()
		if err != nil {
			e.Log.Errorf(msg("read-input.failed"))
			continue
		}
		in = strings.ToLower(line)
		switch in {
		case "", "yes", "y":
			return true, nil
		case "no", "n":
			return false, nil
		}
		e.Printf("%s", msg("configuration.finalize.invalid-input.request-complete", "yes"))
	}
	return false, &Error{Msg: msg("configuration.finalize.failed.invalid-input-request-complete", in)}
}
