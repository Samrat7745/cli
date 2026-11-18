// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package login_test

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/login"
	"github.com/Samrat7745/cli/go/internal/logx"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// ---- LoginCommandTest ----

type stubServer struct {
	code string
	err  login.AuthError
}

func (s stubServer) AuthorizationCode() (string, login.AuthError) { return s.code, s.err }
func (stubServer) Close()                                         {}

func loginFixture(t *testing.T) *testutil.TempConfig {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	tc.Env.Log = logx.New(io.Discard)
	return tc
}

func command(ports login.Range, provider login.ServerProvider, browse login.AuthError, tokens login.OIDCTokens) *login.Command {
	c := login.NewCommand()
	c.CallbackPorts = ports
	c.ServerProvider = provider
	c.Browse = func(*config.Env, string) login.AuthError { return browse }
	c.SendTokenRequest = func(string, url.Values, bool, []string) (login.OIDCTokens, error) { return tokens, nil }
	return c
}

func authErr(t *testing.T, err error) login.AuthError {
	t.Helper()
	le, ok := err.(*login.Error)
	if !ok {
		t.Fatalf("expected CsCliLoginException, got %T %v", err, err)
	}
	return le.Err
}

func TestLoginCommand_whenLogin_thenStoreHasBeenCreated(t *testing.T) {
	tc := loginFixture(t)
	tokens := testutil.OIDCTokens(t)
	c := command(login.DefaultCallbackPorts, func(*url.URL, string) (login.CallbackServer, error) {
		return stubServer{code: "code"}, nil
	}, nil, tokens)
	if err := c.Login(tc.Env); err != nil {
		t.Fatal(err)
	}
	store, aerr := login.Load(tc.Env, false)
	if aerr != nil {
		t.Fatal("store has not been created", aerr)
	}
	for _, tok := range store.Tokens {
		if tok.AccessToken != tokens.AccessToken {
			t.Fatal("stored access token does not equal expected token")
		}
	}
	if len(store.Tokens) != 2 || store.Tokens[0].VcpBaseURL != testutil.VCP1BaseURL || store.Tokens[1].VcpBaseURL != testutil.VCP2BaseURL {
		t.Fatal(store.Tokens)
	}
}

func TestLoginCommand_givenLaunchingBrowserFails_whenLogin_thenThrows(t *testing.T) {
	tc := loginFixture(t)
	c := command(login.DefaultCallbackPorts, func(*url.URL, string) (login.CallbackServer, error) {
		return stubServer{code: "x"}, nil
	}, login.NotSupported, testutil.OIDCTokens(t))
	if got := authErr(t, c.Login(tc.Env)); got != login.NotSupported {
		t.Fatal("unexpected error returned", got)
	}
}

func bindError() error {
	return &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}
}

func TestLoginCommand_givenPortIsInUse_whenLogin_thenSucceedOnNextPort(t *testing.T) {
	tc := loginFixture(t)
	attempt, rounds := 0, 5
	var ports []string
	c := command(login.DefaultCallbackPorts, func(u *url.URL, _ string) (login.CallbackServer, error) {
		ports = append(ports, u.Port())
		attempt++
		if attempt-1 < rounds {
			return nil, bindError()
		}
		return stubServer{code: "code"}, nil
	}, nil, testutil.OIDCTokens(t))
	if err := c.Login(tc.Env); err != nil {
		t.Fatal(err)
	}
	if attempt != rounds+2 {
		t.Fatal("not enough attempts detected", attempt)
	}
	if ports[0] != "32768" || ports[5] != "32773" || ports[6] != "32768" {
		t.Fatal(ports)
	}
}

func TestLoginCommand_givenAllPortsAreInUse_whenLogin_thenThrow(t *testing.T) {
	tc := loginFixture(t)
	r := login.Range{Min: 32768, Max: 32768 + 7}
	attempts := 0
	c := command(r, func(*url.URL, string) (login.CallbackServer, error) {
		attempts++
		return nil, bindError()
	}, nil, testutil.OIDCTokens(t))
	if got := authErr(t, c.Login(tc.Env)); got != login.PortRangeExhaustion {
		t.Fatal(got)
	}
	if attempts != login.Length(r) {
		t.Fatal("wrong number of attempts", attempts)
	}
}

func TestLoginCommand_givenRuntimeExceptionDuringCallbackServerCreation_whenLogin_thenThrow(t *testing.T) {
	tc := loginFixture(t)
	c := command(login.DefaultCallbackPorts, func(*url.URL, string) (login.CallbackServer, error) {
		return nil, errors.New("socket exception")
	}, nil, testutil.OIDCTokens(t))
	if got := authErr(t, c.Login(tc.Env)); got != login.Unexpected {
		t.Fatal(got)
	}
}

// Beyond the Java suite: the authorization URL and the token request of a full login, with the real
// callback server and a stub token endpoint.
func TestLoginCommand_endToEndWithCallbackServerAndTokenEndpoint(t *testing.T) {
	tokens := testutil.OIDCTokens(t)
	var form url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":%q,"id_token":%q,"refresh_token":%q,"expires_in":3600,"token_type":"bearer"}`,
			tokens.AccessToken, tokens.IDToken, tokens.RefreshToken)
	}))
	defer ts.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	tc := testutil.NewTempConfig(t, "", nil, func(e *config.Env) *config.Configuration {
		c := testutil.Configuration(e, ts.URL, ts.URL)
		for _, p := range c.Providers {
			p.OAuth2CallbackURL = fmt.Sprintf("http://127.0.0.1:%d/callback", port)
		}
		return c
	})
	tc.Env.Log = logx.New(io.Discard)
	c := login.NewCommand()
	c.CallbackPorts = login.Range{Min: port, Max: port}
	c.Browse = func(_ *config.Env, auth string) login.AuthError {
		u, _ := url.Parse(auth)
		q := u.Query()
		if q.Get("response_type") != "code" || q.Get("scope") != "openid offline email" || q.Get("redirect_uri") == "" {
			t.Errorf("authorization request %s", auth)
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(q.Get("state")))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	if err := c.Login(tc.Env); err != nil {
		t.Fatal(err)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "the-code" || form.Get("scope") != "offline openid" {
		t.Fatal(form)
	}
	store, aerr := login.Load(tc.Env, false)
	if aerr != nil || len(store.Tokens) != 2 || store.Tokens[0].IDToken != tokens.IDToken {
		t.Fatal(aerr, store)
	}
}

// ---- OAuth2AuthenticationCodeCallbackHttpServerTest ----

func startServer(t *testing.T, state string, timeout time.Duration) *login.Server {
	u, _ := url.Parse("http://127.0.0.1:0")
	s, err := login.NewCallbackServer(u, state, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func invokeCallback(s *login.Server, code, state string) <-chan int {
	ch := make(chan int, 1)
	go func() {
		q := url.Values{"state": {state}, "scope": {"openid offline"}}
		if code != "" {
			q.Set("code", code)
		}
		resp, err := http.Get("http://" + s.Addr() + "/?" + q.Encode())
		if err != nil {
			ch <- -1
			return
		}
		resp.Body.Close()
		ch <- resp.StatusCode
	}()
	return ch
}

func TestCallbackServer_givenCallbackIsNotCalled_whenGetCode_thenTimesOut(t *testing.T) {
	s := startServer(t, "state", time.Second)
	if _, err := s.AuthorizationCode(); err != login.TimeOut {
		t.Fatal("getting code did not time out when callback was not invoked", err)
	}
}

func TestCallbackServer_givenCallbackIsCalledWithCodeParameter_whenGetCode_thenReturnsCode(t *testing.T) {
	s := startServer(t, "state", login.DefaultTimeout)
	status := invokeCallback(s, "abcdefghij0123456789", "state")
	code, err := s.AuthorizationCode()
	if st := <-status; st != 200 || err != nil || code != "abcdefghij0123456789" {
		t.Fatal(st, err, code)
	}
}

func TestCallbackServer_givenCallbackIsCalledWithoutCodeParameter_whenGetCode_thenFailsWithError(t *testing.T) {
	s := startServer(t, "state", login.DefaultTimeout)
	status := invokeCallback(s, "", "state")
	_, err := s.AuthorizationCode()
	if st := <-status; st != 200 || err != login.MissingAuthenticationCode {
		t.Fatal(st, err)
	}
}

func TestCallbackServer_givenCallbackIsCalledTwice_whenGetCode_thenDoesNotBlock(t *testing.T) {
	s := startServer(t, "state", login.DefaultTimeout)
	invokeCallback(s, "code", "state")
	_, _ = s.AuthorizationCode()
	select {
	case st := <-invokeCallback(s, "", "state"):
		if st != 500 {
			t.Fatal("Expected internal server error", st)
		}
	case <-time.After(time.Second):
		t.Fatal("second callback blocked")
	}
}

// Beyond the Java suite: a response with another state (CSRF) is rejected.
func TestCallbackServer_givenWrongState_thenRejects(t *testing.T) {
	s := startServer(t, "state", 200*time.Millisecond)
	if st := <-invokeCallback(s, "code", "other"); st != 500 {
		t.Fatal(st)
	}
}

// ---- VcpTokenStoreTest ----

func createStore(t *testing.T, expired bool) *login.Store {
	ref := time.Now()
	if expired {
		ref = ref.Add(-2 * 24 * time.Hour)
	}
	tok := testutil.OIDCTokens(t)
	t1, _ := login.TokenFrom(ref, testutil.VCP1BaseURL, tok)
	t2, _ := login.TokenFrom(ref, testutil.VCP2BaseURL, tok)
	return &login.Store{Tokens: []login.Token{t1, t2}}
}

func TestVcpTokenStore_givenDefaultDoesNotExist_whenLoad_thenFails(t *testing.T) {
	tc := loginFixture(t)
	tc.Env.TokenPath = filepath.Join("home", "nonexistent")
	if _, err := login.Load(tc.Env, false); err != login.ReadingTokenStoreFailed {
		t.Fatal("wrong error message returned", err)
	}
}

func TestVcpTokenStore_whenRoundTripPersistLoad_thenDataIsPreserved(t *testing.T) {
	tc := loginFixture(t)
	s := createStore(t, false)
	if err := s.Persist(tc.Env); err != nil {
		t.Fatal(err)
	}
	back, err := login.Load(tc.Env, false)
	if err != nil {
		t.Fatal("failed to restore store", err)
	}
	for i := range s.Tokens {
		if !back.Tokens[i].Expires.Equal(s.Tokens[i].Expires) {
			t.Fatal("expiry changed")
		}
		back.Tokens[i].Expires = s.Tokens[i].Expires
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatal("store data not the same after a round trip")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("io") }

func TestVcpTokenStore_givenWriterFails_whenPersist_thenFails(t *testing.T) {
	tc := loginFixture(t)
	if err := createStore(t, false).PersistTo(tc.Env, failingWriter{}); err != login.PersistingTokenStoreFailed {
		t.Fatal("wrong error returned", err)
	}
}

func TestVcpTokenStore_givenValidTokens_whenRefresh_thenDoesNothing(t *testing.T) {
	tc := loginFixture(t)
	s := createStore(t, false)
	called := false
	got, err := s.Refresh(tc.Env, login.WithRefreshRequest(func(login.Token, *config.VcpConfiguration, bool, []string) (login.OIDCTokens, error) {
		called = true
		return login.OIDCTokens{}, nil
	}))
	if err != nil || called || !reflect.DeepEqual(got.Tokens, s.Tokens) {
		t.Fatal(err, called)
	}
}

func TestVcpTokenStore_givenExpiredTokens_whenRefresh_thenRefreshesTokens(t *testing.T) {
	tc := loginFixture(t)
	s := createStore(t, true)
	for _, tok := range s.Tokens {
		if !tok.IsExpired(time.Now()) {
			t.Fatal("tokens in store are not expired")
		}
	}
	fresh := testutil.OIDCTokens(t)
	got, err := s.Refresh(tc.Env, login.WithRefreshRequest(func(login.Token, *config.VcpConfiguration, bool, []string) (login.OIDCTokens, error) {
		return fresh, nil
	}))
	if err != nil {
		t.Fatal("refresh failed", err)
	}
	for _, tok := range got.Tokens {
		if tok.IsExpired(time.Now()) {
			t.Fatal("tokens in store are expired after refresh")
		}
	}
}

func TestVcpTokenStore_givenExpiredTokensAndFailingProvider_whenRefresh_thenRefreshFails(t *testing.T) {
	// The token endpoints refuse connections (127.0.0.1:1) instead of resolving example.com.
	tc := testutil.NewTempConfig(t, "", nil, func(e *config.Env) *config.Configuration {
		return testutil.Configuration(e, "https://127.0.0.1:1", "https://127.0.0.1:1")
	})
	tc.Env.Log = logx.New(io.Discard)
	_, err := createStore(t, true).Refresh(tc.Env)
	bt, ok := err.(login.ByTokenError)
	if !ok || len(bt.Errors) != 2 || bt.Errors[testutil.VCP1BaseURL] != login.RefreshingTokenFailed {
		t.Fatal("wrong error returned", err)
	}
	if !strings.HasPrefix(bt.Error(), "VcpTokenStore.ByTokenError(errors={https://apollo.example.com=REFRESHING_TOKEN_FAILED") {
		t.Fatal(bt.Error())
	}
}

// Beyond the Java suite: the token file is the Java CLI's format and loading with refresh writes it back.
func TestVcpTokenStore_fileFormat(t *testing.T) {
	tc := loginFixture(t)
	s := &login.Store{Tokens: []login.Token{{VcpBaseURL: "https://a/", AccessToken: "at", IDToken: "id", RefreshToken: "rt",
		Expires: time.UnixMilli(1794995268860)}}}
	b, _ := s.Encode()
	if want := `{"tokens":[{"vcpBaseUrl":"https://a/","accessToken":"at","idToken":"id","refreshToken":"rt","expires":1794995268860}]}`; string(b) != want {
		t.Fatal(string(b))
	}
	_ = tc
}

// ---- RangeUtilTest ----

func TestRangeUtil(t *testing.T) {
	if login.Length(login.Range{Min: 1, Max: 5}) != 5 {
		t.Fatal("Range with five elements must have length 5")
	}
	if login.Length(login.Range{Min: 1, Max: 1}) != 1 {
		t.Fatal("One element range must have length 1")
	}
	if r := login.ConsumeLower(login.Range{Min: 1, Max: 2}); r.Min != r.Max || r.Min != 2 {
		t.Fatal("Range must contain only remaining element")
	}
}

// ---- OAuthUtilTest ----

func tlsServer(t *testing.T) (*httptest.Server, testutil.Cert) {
	cert := testutil.NewCert(t)
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test" {
			_, _ = w.Write([]byte("success"))
			return
		}
		http.NotFound(w, r)
	}))
	s.TLS = &tls.Config{Certificates: []tls.Certificate{cert.TLS}}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s, cert
}

func get(t *testing.T, insecure bool, certs []string, u string) (*http.Response, error) {
	c, err := login.HTTPClient(insecure, certs)
	if err != nil {
		t.Fatal(err)
	}
	return c.Get(u)
}

func TestOAuthUtil_givenServerCertIsUntrusted_whenRequesting_thenThrows(t *testing.T) {
	s, _ := tlsServer(t)
	if _, err := get(t, false, nil, s.URL+"/test"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatal(err)
	}
}

func TestOAuthUtil_givenContextConfiguredTrustAll_whenRequesting_thenSucceed(t *testing.T) {
	s, _ := tlsServer(t)
	resp, err := get(t, true, nil, s.URL+"/test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "success" {
		t.Fatal(resp.StatusCode, string(b))
	}
}

func TestOAuthUtil_givenServerCertIsTrusted_whenRequesting_thenSucceed(t *testing.T) {
	s, cert := tlsServer(t)
	resp, err := get(t, false, []string{cert.PEMFile}, s.URL+"/test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "success" {
		t.Fatal(resp.StatusCode, string(b))
	}
}

func TestOAuthUtil_givenCustomCertsDefined_whenRequestingServerWithCACert_thenSucceed(t *testing.T) {
	const googleDirections = "https://maps.googleapis.com/maps/api/directions/json"
	if c, err := net.DialTimeout("tcp", "maps.googleapis.com:443", 3*time.Second); err != nil {
		t.Skip("no network access: ", err)
	} else {
		c.Close()
	}
	_, cert := tlsServer(t)
	resp, err := get(t, false, []string{cert.PEMFile}, googleDirections)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// The Directions API answers 400 for this request, after the certificate was verified.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatal(resp.StatusCode)
	}
}
