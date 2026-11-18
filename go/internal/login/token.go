// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package login is the OAuth2 login of the CLI (Java io.carbynestack.cli.login): the "login" command,
// the local callback server, and the token store in ~/.cs/access-tokens.json.
package login

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	httpclient "github.com/Samrat7745/java-http-client"

	"github.com/Samrat7745/cli/go/internal/config"
)

// AuthError is an AuthenticationError: an enum constant of the Java CLI or a ByTokenError.
type AuthError interface {
	error
	authError()
}

// Code is an AuthenticationError enum constant (LoginCommandError, CallbackError, BrowserLaunchError,
// VcpTokenStoreErrors); its text is the constant name.
type Code string

func (c Code) Error() string { return string(c) }
func (Code) authError()      {}

// The Java enum constants.
const (
	// LoginCommandError
	FailedToGetAccessToken Code = "FAILED_TO_GET_ACCESS_TOKEN"
	PortRangeExhaustion    Code = "PORT_RANGE_EXHAUSTION"
	Unexpected             Code = "UNEXPECTED"
	// CallbackError (UNEXPECTED is shared)
	MissingAuthenticationCode Code = "MISSING_AUTHENTICATION_CODE"
	Interrupted               Code = "INTERRUPTED"
	TimeOut                   Code = "TIME_OUT"
	// BrowserLaunchError (INTERRUPTED is shared)
	NonZeroExitCode Code = "NON_ZERO_EXIT_CODE"
	NotSupported    Code = "NOT_SUPPORTED"
	Generic         Code = "GENERIC"
	// VcpTokenStoreErrors
	ReadingTokenStoreFailed     Code = "READING_TOKEN_STORE_FAILED"
	PersistingTokenStoreFailed  Code = "PERSISTING_TOKEN_STORE_FAILED"
	VcpConfigurationUnavailable Code = "VCP_CONFIGURATION_UNAVAILABLE"
	RefreshingTokenFailed       Code = "REFRESHING_TOKEN_FAILED"
)

// ByTokenError holds the refresh error of every failed token, keyed by the VCP base URL.
type ByTokenError struct{ Errors map[string]AuthError }

func (ByTokenError) authError() {}

func (e ByTokenError) Error() string {
	keys := make([]string, 0, len(e.Errors))
	for k := range e.Errors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + e.Errors[k].Error()
	}
	return "VcpTokenStore.ByTokenError(errors={" + strings.Join(parts, ", ") + "})"
}

// Error is CsCliLoginException: its message is the authentication error.
type Error struct{ Err AuthError }

func (e *Error) Error() string { return e.Err.Error() }

// Message is the exception message.
func (e *Error) Message() string { return e.Err.Error() }

// Token is a VcpToken: the OAuth2 tokens of one virtual cloud provider.
type Token struct {
	VcpBaseURL   string
	AccessToken  string
	IDToken      string
	RefreshToken string
	Expires      time.Time
}

// OIDCTokens is a token endpoint response.
type OIDCTokens struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// TokenFrom is VcpToken.from(created, vcpBaseUrl, tokens): the token expires when the access token's
// lifetime has passed since created.
func TokenFrom(created time.Time, vcpBaseURL string, t OIDCTokens) (Token, error) {
	if t.RefreshToken == "" {
		return Token{}, errors.New("no refresh token in the token response")
	}
	return Token{VcpBaseURL: vcpBaseURL, AccessToken: t.AccessToken, IDToken: t.IDToken, RefreshToken: t.RefreshToken,
		Expires: created.Add(time.Duration(t.ExpiresIn) * time.Second).Truncate(time.Millisecond)}, nil
}

// IsExpired reports whether the token expired at now.
func (t Token) IsExpired(now time.Time) bool { return now.After(t.Expires) }

type tokenJSON struct {
	VcpBaseURL   *string `json:"vcpBaseUrl"`
	AccessToken  *string `json:"accessToken"`
	IDToken      *string `json:"idToken"`
	RefreshToken *string `json:"refreshToken"`
	Expires      *int64  `json:"expires"`
}

// Store is the VcpTokenStore.
type Store struct {
	Tokens []Token
}

// DefaultLocation is the token file: the custom location or ~/.cs/access-tokens.json.
func DefaultLocation(e *config.Env) string {
	if e.TokenPath != "" {
		return e.TokenPath
	}
	return filepath.Join(e.Home, ".cs", "access-tokens.json")
}

// Exists reports whether the token file exists.
func Exists(e *config.Env) bool {
	_, err := os.Stat(DefaultLocation(e))
	return err == nil
}

// Load reads the token file; with refresh, the tokens are refreshed and the store is written back.
func Load(e *config.Env, refresh bool, opts ...Option) (*Store, AuthError) {
	f, err := os.Open(DefaultLocation(e))
	if err != nil {
		e.Log.Error("reading token store failed", err)
		return nil, ReadingTokenStoreFailed
	}
	defer f.Close()
	return LoadFrom(e, f, refresh, opts...)
}

// LoadFrom reads a token store from r (VcpTokenStore.load(Reader, boolean)).
func LoadFrom(e *config.Env, r io.Reader, refresh bool, opts ...Option) (*Store, AuthError) {
	s, err := decode(r)
	if err != nil {
		e.Log.Error("reading token store failed", err)
		return nil, ReadingTokenStoreFailed
	}
	if !refresh {
		return s, nil
	}
	refreshed, aerr := s.Refresh(e, opts...)
	if aerr != nil {
		return nil, aerr
	}
	_ = refreshed.Persist(e)
	return refreshed, nil
}

func decode(r io.Reader) (*Store, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("No content to map due to end-of-input")
	}
	var raw struct {
		Tokens []tokenJSON `json:"tokens"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&raw); err != nil {
		return nil, err
	}
	s := &Store{Tokens: []Token{}}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for _, t := range raw.Tokens {
		tok := Token{VcpBaseURL: str(t.VcpBaseURL), AccessToken: str(t.AccessToken), IDToken: str(t.IDToken),
			RefreshToken: str(t.RefreshToken)}
		if t.Expires != nil {
			tok.Expires = time.UnixMilli(*t.Expires)
		}
		s.Tokens = append(s.Tokens, tok)
	}
	return s, nil
}

// Encode is the Jackson serialization of the store.
func (s *Store) Encode() ([]byte, error) {
	type out struct {
		VcpBaseURL   string `json:"vcpBaseUrl"`
		AccessToken  string `json:"accessToken"`
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		Expires      int64  `json:"expires"`
	}
	doc := struct {
		Tokens []out `json:"tokens"`
	}{Tokens: []out{}}
	for _, t := range s.Tokens {
		doc.Tokens = append(doc.Tokens, out{t.VcpBaseURL, t.AccessToken, t.IDToken, t.RefreshToken, t.Expires.UnixMilli()})
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// PersistTo writes the store to w.
func (s *Store) PersistTo(e *config.Env, w io.Writer) AuthError {
	b, err := s.Encode()
	if err == nil {
		_, err = w.Write(b)
	}
	if err != nil {
		e.Log.Error("persisting token store failed: %s", err)
		return PersistingTokenStoreFailed
	}
	return nil
}

// Persist writes the store to the token file.
func (s *Store) Persist(e *config.Env) AuthError {
	f, err := os.Create(DefaultLocation(e))
	if err != nil {
		e.Log.Error("persisting token store failed", err)
		return PersistingTokenStoreFailed
	}
	defer f.Close()
	return s.PersistTo(e, f)
}

// Option changes how tokens are refreshed (tests replace the token request).
type Option func(*refresher)

type refresher struct {
	send func(t Token, v *config.VcpConfiguration, insecure bool, certs []string) (OIDCTokens, error)
}

// WithRefreshRequest replaces the refresh token request.
func WithRefreshRequest(f func(t Token, v *config.VcpConfiguration, insecure bool, certs []string) (OIDCTokens, error)) Option {
	return func(r *refresher) { r.send = f }
}

// Refresh refreshes every token that is valid for less than one hour (all of them, with Hydra's one
// hour access tokens). It fails with a ByTokenError if any refresh fails.
func (s *Store) Refresh(e *config.Env, opts ...Option) (*Store, AuthError) {
	r := &refresher{send: SendRefreshToken}
	for _, o := range opts {
		o(r)
	}
	cfg, err := e.Instance()
	if err != nil {
		e.Log.Error("failed to read VC configuration", err)
		return nil, ReadingTokenStoreFailed
	}
	if len(s.Tokens) == 0 {
		// Java: groupingBy yields no "true" group and the stream access throws a NullPointerException.
		e.Log.Error("refreshing token store failed", errors.New("java.lang.NullPointerException"))
		return nil, ReadingTokenStoreFailed
	}
	failed := map[string]AuthError{}
	var ok []Token
	for _, t := range s.Tokens {
		nt, aerr := r.refresh(e, t, cfg)
		if aerr != nil {
			failed[t.VcpBaseURL] = aerr
		} else {
			ok = append(ok, nt)
		}
	}
	if len(failed) > 0 {
		return nil, ByTokenError{Errors: failed}
	}
	return &Store{Tokens: ok}, nil
}

func (r *refresher) refresh(e *config.Env, t Token, cfg *config.Configuration) (Token, AuthError) {
	var vcp *config.VcpConfiguration
	for _, p := range cfg.Providers {
		if p.BaseURL == t.VcpBaseURL {
			vcp = p
			break
		}
	}
	if vcp == nil {
		e.Log.Errorf("no matching configuration for provider with base URL {}", t.VcpBaseURL)
		return Token{}, VcpConfigurationUnavailable
	}
	if t.Expires.Sub(e.Now()) >= time.Hour {
		return t, nil
	}
	e.Log.Debugf("refreshing token for VCP with base URL {}", t.VcpBaseURL)
	tokens, err := r.send(t, vcp, cfg.IsNoSSLValidation(), cfg.TrustedCertificates)
	var nt Token
	if err == nil {
		nt, err = TokenFrom(e.Now(), vcp.BaseURL, tokens)
	}
	if err != nil {
		e.Log.Error(fmt.Sprintf("refreshing token for provider with URL %s failed", t.VcpBaseURL), err)
		return Token{}, RefreshingTokenFailed
	}
	return nt, nil
}

// SendRefreshToken requests new tokens with the refresh token grant.
func SendRefreshToken(t Token, v *config.VcpConfiguration, insecure bool, certs []string) (OIDCTokens, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.RefreshToken},
		"scope":         {"offline openid"},
		"client_id":     {v.ClientID()},
	}
	return tokenRequest(v.TokenEndpointURI(), form, insecure, certs, true)
}

// HTTPClient returns the client for the OAuth2 endpoints (OAuthUtil.setSslContextForRequestWithConfiguration):
// trust all with insecure, otherwise the system roots plus the trusted certificates.
func HTTPClient(insecure bool, certs []string) (*http.Client, error) {
	c, err := httpclient.New[string](
		httpclient.WithoutSSLValidation(insecure),
		httpclient.WithTrustedCertificates(certs...),
	)
	if err != nil {
		return nil, err
	}
	return c.HTTPClient(), nil
}

// TokenError is a TokenErrorResponse of the token endpoint.
type TokenError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
	Status      int
}

func (e *TokenError) Error() string {
	s := e.Code
	if e.Description != "" {
		s += ": " + e.Description
	}
	return s
}

// tokenRequest posts form to endpoint. With strict200 (OIDCTokenResponse.parse) every status but 200
// is an error; otherwise (OIDCTokenResponseParser) a JSON error body is returned as *TokenError.
func tokenRequest(endpoint string, form url.Values, insecure bool, certs []string, strict200 bool) (OIDCTokens, error) {
	hc, err := HTTPClient(insecure, certs)
	if err != nil {
		return OIDCTokens{}, err
	}
	resp, err := hc.PostForm(endpoint, form)
	if err != nil {
		return OIDCTokens{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OIDCTokens{}, err
	}
	if resp.StatusCode != http.StatusOK {
		if !strict200 {
			te := &TokenError{Status: resp.StatusCode}
			if json.Unmarshal(body, te) == nil && te.Code != "" {
				return OIDCTokens{}, te
			}
		}
		return OIDCTokens{}, fmt.Errorf("Unexpected HTTP status code %d, must be [200]", resp.StatusCode)
	}
	var t OIDCTokens
	if err := json.Unmarshal(body, &t); err != nil {
		return OIDCTokens{}, err
	}
	if t.AccessToken == "" {
		return OIDCTokens{}, errors.New("Missing JSON object member with key access_token")
	}
	if t.TokenType == "" {
		return OIDCTokens{}, errors.New("Missing JSON object member with key token_type")
	}
	return t, nil
}
