// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"

	ephemeral "github.com/Samrat7745/ephemeral/pkg/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// EphemeralClientName is the ephemeral client.
const EphemeralClientName = "ephemeral"

// ExecuteCommand is the ephemeral execute command.
const ExecuteCommand = "execute"

// EphemeralMulti is the part of the Ephemeral multi client the CLI uses.
type EphemeralMulti interface {
	Execute(ctx context.Context, code string, inputSecretIDs []uuid.UUID) ([]ephemeral.Result, error)
}

// EphemeralConfig is the parsed command (EphemeralClientCliCommandConfig).
type EphemeralConfig struct {
	Application string
	// Timeout is the -t option in seconds. Like the Java CLI, it is parsed but not applied.
	Timeout int
	Inputs  []uuid.UUID
}

// EphemeralFactory creates the client for a command (EphemeralClientFactory); nil uses the configuration.
type EphemeralFactory func(cfg *EphemeralConfig) (EphemeralMulti, error)

func ep(key string, args ...any) string { return props.Format(props.Ephemeral, key, args...) }

// EphemeralClient registers the ephemeral client.
func EphemeralClient(factory EphemeralFactory) Client {
	return Client{Name: EphemeralClientName, Description: props.Get(props.Ephemeral, "client-description"),
		New: func(e *config.Env, args []string, debug bool) *ClientCli {
			return NewEphemeralCli(e, args, debug, factory)
		}}
}

// NewEphemeralCli is EphemeralClientCli.
func NewEphemeralCli(e *config.Env, args []string, debug bool, factory EphemeralFactory) *ClientCli {
	c := &ClientCli{Env: e, Name: EphemeralClientName, Args: args, Debug: debug}
	var help bool
	cfg := &EphemeralConfig{Timeout: 10, Inputs: []uuid.UUID{}}
	c.Commands = []*ClientCommand{{Name: ExecuteCommand,
		Description: props.Get(props.Ephemeral, "execute.command-description"),
		Main: &jc.Main{Description: props.Get(props.Ephemeral, "option.application-description"), Required: true, Arity: -1,
			Set: func(v string) error { cfg.Application = v; return nil }},
		Options: []*jc.Option{
			hiddenHelp(&help),
			{Names: []string{"-t", "--timeout"}, Description: props.Get(props.Ephemeral, "option.timeout"), Order: 1,
				Kind: jc.Value, Arity: -1, Default: "10", HasDefault: true,
				Set: func(name, v string) error {
					n, err := javautil.ParseInt(v)
					if err != nil {
						return &jc.ParameterError{Msg: "\"" + name + "\": couldn't convert \"" + v + "\" to an integer"}
					}
					cfg.Timeout = n
					return nil
				}},
			{Names: []string{"-i", "--input"}, Description: props.Get(props.Ephemeral, "execute.option.input-description"),
				Order: 1, Kind: jc.List, Arity: -1, Split: true, Default: "[]", HasDefault: true,
				Reset: func() { cfg.Inputs = nil },
				Set: func(_, v string) error {
					s, err := parseUUIDArg(v)
					if err == nil {
						cfg.Inputs = append(cfg.Inputs, uuid.MustParse(s))
					}
					return err
				}},
		},
		NewRunner: func() (Runner, error) {
			r, err := newEphemeralRunner(e, cfg, factory)
			if err != nil {
				return nil, err
			}
			return runFunc(r.run), nil
		}}}
	return c
}

type ephemeralRunner struct {
	env    *config.Env
	cfg    *EphemeralConfig
	client EphemeralMulti
}

// newEphemeralRunner is the EphemeralClientCliCommandRunner constructor; every failure while
// creating the client (including reading the tokens) is a client instantiation failure.
func newEphemeralRunner(e *config.Env, c *EphemeralConfig, factory EphemeralFactory) (*ephemeralRunner, error) {
	cfg, err := e.Instance()
	if err != nil {
		return nil, err
	}
	fail := func(err error) error {
		return runnerErr(props.Get(props.Ephemeral, "client-instantiation-failed"), err)
	}
	tokens, err := tokensByBaseURL(e)
	if err != nil {
		return nil, fail(err)
	}
	byEndpoint := map[ephemeral.Endpoint]string{}
	for base, t := range tokens {
		p, err := providerFor(cfg, base)
		if err != nil {
			return nil, fail(err)
		}
		byEndpoint[ephemeral.Endpoint{ServiceURI: p.EphemeralURL(), Application: c.Application}] = t.IDToken
	}
	var cl EphemeralMulti
	if factory != nil {
		cl, err = factory(c)
	} else {
		o := ephemeral.Options{NoSSLValidation: cfg.IsNoSSLValidation()}
		for _, p := range cfg.Providers {
			o.Endpoints = append(o.Endpoints, ephemeral.Endpoint{ServiceURI: p.EphemeralURL(), Application: c.Application})
		}
		if tokens != nil {
			o.BearerTokenProvider = func(ep ephemeral.Endpoint) string { return byEndpoint[ep] }
		}
		if !cfg.IsNoSSLValidation() {
			o.TrustedCertificates = cfg.TrustedCertificates
		}
		cl, err = ephemeral.NewMulti(o)
	}
	if err != nil {
		return nil, fail(err)
	}
	return &ephemeralRunner{env: e, cfg: c, client: cl}, nil
}

func (r *ephemeralRunner) run() error {
	r.env.Println("Provide program to execute. Press Ctrl+D to submit.")
	code, err := r.env.ReadAll()
	if err != nil {
		return runnerErr(props.Get(props.Ephemeral, "execute.failure.read-code"), err)
	}
	results, err := r.client.Execute(context.Background(), code, r.cfg.Inputs)
	if err != nil {
		var ae *ephemeral.ActivationError
		if errors.As(err, &ae) {
			return runnerErr(ep("execute.failure.invoke-status-code", ae.ResponseCode, ae.Message), nil)
		}
		return runnerErr(props.Get(props.Ephemeral, "execute.failure.invoke"), err)
	}
	if len(results) == 0 {
		return runnerErr("Index 0 out of bounds for length 0", nil)
	}
	first := results[0].Response
	for _, res := range results {
		if !sameElements(first, res.Response) {
			msg := props.Get(props.Ephemeral, "execute.failure.invoke-different-results")
			return runnerErr(msg, errors.New(msg))
		}
	}
	out := javautil.ListString(first)
	r.env.Log.Debugf(ep("execute.log.success", out))
	r.env.Println(out)
	return nil
}

// sameElements is a.size() == b.size() && a.containsAll(b).
func sameElements(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if !set[x] {
			return false
		}
	}
	return true
}
