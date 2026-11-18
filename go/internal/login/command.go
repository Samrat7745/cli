// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// CommandName is the login command.
const CommandName = "login"

// AuthenticationCodeTimeout is how long the login waits for the browser callback.
const AuthenticationCodeTimeout = 120 * time.Second

// Range is an inclusive port range (commons-lang Range<Integer>).
type Range struct{ Min, Max int }

// DefaultCallbackPorts is the ephemeral port range used by many Linux kernels.
var DefaultCallbackPorts = Range{32768, 61000}

// ConsumeLower drops the lowest port (RangeUtils.consumeLower).
func ConsumeLower(r Range) Range { return Range{r.Min + 1, r.Max} }

// Length is the number of ports in r (RangeUtils.getLength).
func Length(r Range) int { return r.Max - r.Min + 1 }

// CallbackServer receives the authorization code.
type CallbackServer interface {
	AuthorizationCode() (string, AuthError)
	Close()
}

// ServerProvider starts a callback server on callbackURL that accepts responses carrying state. An
// error that wraps syscall.EADDRINUSE (Java BindException) makes the login try the next port.
type ServerProvider func(callbackURL *url.URL, state string) (CallbackServer, error)

// Command is the "login" command (LoginCommand).
type Command struct {
	Help bool

	// CallbackPorts and ServerProvider are the LoginCommand constructor arguments.
	CallbackPorts  Range
	ServerProvider ServerProvider
	// Browse opens the authorization URL (BrowserLauncher.browse); nil error means it was opened.
	Browse func(e *config.Env, u string) AuthError
	// SendTokenRequest exchanges the authorization code (LoginCommand.sendTokenRequest).
	SendTokenRequest func(endpoint string, form url.Values, insecure bool, certs []string) (OIDCTokens, error)
}

// NewCommand returns the command with the default port range and callback server.
func NewCommand() *Command {
	return &Command{
		CallbackPorts: DefaultCallbackPorts,
		ServerProvider: func(u *url.URL, state string) (CallbackServer, error) {
			return NewCallbackServer(u, state, AuthenticationCodeTimeout)
		},
		Browse: BrowseURL,
		SendTokenRequest: func(endpoint string, form url.Values, insecure bool, certs []string) (OIDCTokens, error) {
			return tokenRequest(endpoint, form, insecure, certs, false)
		},
	}
}

// Options returns the command's parser options.
func (c *Command) Options() []*jc.Option {
	return []*jc.Option{{Names: []string{"--help"}, Description: props.Get(props.Login, "option.help-description"),
		Help: true, Order: -1, Kind: jc.Bool, Arity: -1, Flip: func() { c.Help = !c.Help }}}
}

// Description is the command description.
func (c *Command) Description() string { return props.Get(props.Login, "command-description") }

// Login logs in to every configured provider and stores the tokens.
func (c *Command) Login(e *config.Env) error {
	if c.Help {
		e.Println(props.UnescapeJava(props.Get(props.Login, "detailed-help")))
		return nil
	}
	cfg, err := e.Instance()
	if err != nil {
		return err
	}
	var tokens []Token
	for _, v := range cfg.Providers {
		t, aerr := c.login(e, v, c.CallbackPorts, cfg.IsNoSSLValidation(), cfg.TrustedCertificates)
		if aerr != nil {
			return &Error{Err: aerr}
		}
		tokens = append(tokens, t)
	}
	if aerr := (&Store{Tokens: tokens}).Persist(e); aerr != nil {
		return &Error{Err: aerr}
	}
	return nil
}

func (c *Command) login(e *config.Env, v *config.VcpConfiguration, ports Range, insecure bool, certs []string) (Token, AuthError) {
	callback, err := url.Parse(v.CallbackURL())
	if err != nil {
		panic(err) // Java: IllegalStateException
	}
	listen := *callback
	listen.Host = callback.Hostname() + ":" + strconv.Itoa(ports.Min)
	e.Log.Debugf("launching callback server on local address {}", listen.String())
	state := randomValue()
	server, err := c.ServerProvider(&listen, state)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			e.Log.Debugf("opening local callback server on local address {} failed", listen.String())
			if Length(ports) <= 1 {
				return Token{}, PortRangeExhaustion
			}
			return c.login(e, v, ConsumeLower(ports), insecure, certs)
		}
		e.Log.Error("login failed for unexpected reason", err)
		return Token{}, Unexpected
	}
	defer server.Close()
	auth := v.AuthEndpointURI() + "?" + encode([][2]string{
		{"response_type", "code"},
		{"client_id", v.ClientID()},
		{"redirect_uri", v.CallbackURL()},
		{"scope", "openid offline email"},
		{"state", state},
		{"nonce", randomValue()},
	})
	if aerr := c.Browse(e, auth); aerr != nil {
		return Token{}, aerr
	}
	code, aerr := server.AuthorizationCode()
	if aerr != nil {
		return Token{}, aerr
	}
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {v.CallbackURL()},
		"scope":        {"offline openid"},
		"client_id":    {v.ClientID()},
	}
	tokens, err := c.SendTokenRequest(v.TokenEndpointURI(), form, insecure, certs)
	if err != nil {
		e.Log.Error("getting access token failed unexpectedly", err)
		return Token{}, FailedToGetAccessToken
	}
	t, err := TokenFrom(e.Now(), v.BaseURL, tokens)
	if err != nil {
		e.Log.Error("getting access token failed unexpectedly", err)
		return Token{}, FailedToGetAccessToken
	}
	return t, nil
}

func encode(params [][2]string) string {
	s := ""
	for i, p := range params {
		if i > 0 {
			s += "&"
		}
		s += url.QueryEscape(p[0]) + "=" + url.QueryEscape(p[1])
	}
	return s
}

// randomValue is a Nimbus State/Nonce: 32 random bytes, base64url without padding.
func randomValue() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// BrowseURL opens u in the browser (BrowserLauncher.browse): xdg-open on Linux, the desktop's opener
// elsewhere.
func BrowseURL(e *config.Env, u string) AuthError {
	e.Log.Debugf("launching browser with URL {}", u)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return NotSupported
		}
		cmd = exec.Command("xdg-open", u)
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return NotSupported
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			e.Log.Debugf("launched browser using {} with exit code {}", cmd.Path, ee.ExitCode())
			return NonZeroExitCode
		}
		e.Log.Error("Failed to open browser for URL "+u, err)
		return Generic
	}
	e.Log.Debugf("launched browser using {} with exit code {}", cmd.Path, 0)
	return nil
}
