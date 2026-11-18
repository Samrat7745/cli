// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"

	thymus "github.com/Samrat7745/thymus/services/pkg/client"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// ThymusClientName is the thymus client.
const ThymusClientName = "thymus"

// Thymus command names.
const (
	ListPoliciesCommand = "list-policies"
	GetPolicyCommand    = "get-policy"
)

// ThymusVC is the part of the Thymus VC client the CLI uses.
type ThymusVC interface {
	GetPolicies(ctx context.Context) ([]thymus.NamespacedName, error)
	GetPolicy(ctx context.Context, name thymus.NamespacedName) (*thymus.Policy, error)
}

// ThymusFactory creates the client (ThymusClientFactory); nil uses the configuration.
type ThymusFactory func() (ThymusVC, error)

func th(key string, args ...any) string { return props.Format(props.Thymus, key, args...) }

// ThymusClient registers the thymus client.
func ThymusClient(factory ThymusFactory) Client {
	return Client{Name: ThymusClientName, Description: props.Get(props.Thymus, "client-description"),
		New: func(e *config.Env, args []string, debug bool) *ClientCli {
			return NewThymusCli(e, args, debug, factory)
		}}
}

// NewThymusCli is ThymusClientCli.
func NewThymusCli(e *config.Env, args []string, debug bool, factory ThymusFactory) *ClientCli {
	c := &ClientCli{Env: e, Name: ThymusClientName, Args: args, Debug: debug}
	runner := func(run func(cl ThymusVC) error) func() (Runner, error) {
		return func() (Runner, error) {
			cl, err := newThymusClient(e, factory)
			if err != nil {
				return nil, err
			}
			return runFunc(func() error { return run(cl) }), nil
		}
	}
	var listHelp, getHelp bool
	var names []thymus.NamespacedName
	c.Commands = []*ClientCommand{
		{Name: ListPoliciesCommand, Description: props.Get(props.Thymus, "list-policies.command-description"),
			Options: []*jc.Option{hiddenHelp(&listHelp)},
			NewRunner: runner(func(cl ThymusVC) error {
				res, err := cl.GetPolicies(context.Background())
				if err != nil {
					return runnerErr(th("list-policies.failure", err.Error()), nil)
				}
				for _, n := range res {
					e.Println(n.String())
				}
				return nil
			})},
		{Name: GetPolicyCommand, Description: props.Get(props.Thymus, "get-policy.command-description"),
			Main: &jc.Main{Description: props.Get(props.Thymus, "get-policy.parameter.policy-id-description"), Required: true,
				Arity: 1, List: true, Reset: func() { names = nil },
				Set: func(v string) error {
					n, err := thymus.ParseNamespacedName(v)
					if err != nil {
						return &jc.ParameterError{Msg: th("policy-id.conversion.exception", v)}
					}
					names = append(names, n)
					return nil
				},
				Count: func() int { return len(names) }},
			Options: []*jc.Option{hiddenHelp(&getHelp)},
			NewRunner: runner(func(cl ThymusVC) error {
				p, err := cl.GetPolicy(context.Background(), names[0])
				if err != nil {
					return runnerErr(th("get-policy.failure", err.Error()), nil)
				}
				e.Println(p.Source)
				return nil
			})},
	}
	return c
}

// newThymusClient is the ThymusClientCliCommandRunner constructor; every failure while creating the
// client (including reading the tokens) is a client instantiation failure.
func newThymusClient(e *config.Env, factory ThymusFactory) (ThymusVC, error) {
	cfg, err := e.Instance()
	if err != nil {
		return nil, err
	}
	fail := func(err error) error { return runnerErr(props.Get(props.Thymus, "client-instantiation-failed"), err) }
	tokens, err := tokensByBaseURL(e)
	if err != nil {
		return nil, fail(err)
	}
	byEndpoint := map[thymus.Endpoint]string{}
	for base, t := range tokens {
		p, err := providerFor(cfg, base)
		if err != nil {
			return nil, fail(err)
		}
		byEndpoint[thymus.Endpoint{ServiceURI: p.ThymusURL()}] = t.IDToken
	}
	var cl ThymusVC
	if factory != nil {
		cl, err = factory()
	} else {
		o := thymus.Options{NoSSLValidation: cfg.IsNoSSLValidation()}
		for _, p := range cfg.Providers {
			o.Endpoints = append(o.Endpoints, thymus.Endpoint{ServiceURI: p.ThymusURL()})
		}
		if tokens != nil {
			o.BearerTokenProvider = func(ep thymus.Endpoint) string { return byEndpoint[ep] }
		}
		if !cfg.IsNoSSLValidation() {
			o.TrustedCertificates = cfg.TrustedCertificates
		}
		cl, err = thymus.NewVCClient(o)
	}
	if err != nil {
		return nil, fail(err)
	}
	return cl, nil
}
