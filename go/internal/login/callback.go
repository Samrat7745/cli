// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultTimeout is how long the callback server waits (OAuth2AuthenticationCodeCallbackHttpServer.DEFAULT_TIMEOUT_MILLIS).
const DefaultTimeout = 120 * time.Second

type callbackResult struct {
	code string
	err  AuthError
}

// Server is the local HTTP server that receives the OAuth2 authorization response
// (OAuth2AuthenticationCodeCallbackHttpServer). The first valid response is handed to
// AuthorizationCode; later ones, responses with a wrong state and error responses get HTTP 500.
type Server struct {
	srv         *http.Server
	ln          net.Listener
	timeout     time.Duration
	state       string
	queue       chan callbackResult
	mu          sync.Mutex
	transferred bool
}

// NewCallbackServer listens on the host and port of callbackURL. A port in use is reported as an
// error wrapping syscall.EADDRINUSE.
func NewCallbackServer(callbackURL *url.URL, state string, timeout time.Duration) (*Server, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(callbackURL.Hostname(), callbackURL.Port()))
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, timeout: timeout, state: state, queue: make(chan callbackResult)}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	// Nimbus AuthenticationResponseParser: an "error" parameter is an error response; the state must match.
	if q.Has("error") || !q.Has("state") || q.Get("state") != s.state {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	res := callbackResult{code: q.Get("code")}
	if res.code == "" {
		res = callbackResult{err: MissingAuthenticationCode}
	}
	if !s.transferred && s.tryTransfer(res) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Authorization code received. You can close this window now."))
		s.transferred = true
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
}

// tryTransfer hands res to a waiting AuthorizationCode call, waiting up to the timeout for one
// (TransferQueue.tryTransfer).
func (s *Server) tryTransfer(res callbackResult) bool {
	t := time.NewTimer(s.timeout)
	defer t.Stop()
	select {
	case s.queue <- res:
		return true
	case <-t.C:
		return false
	}
}

// AuthorizationCode waits up to the timeout for the authorization code.
func (s *Server) AuthorizationCode() (string, AuthError) {
	t := time.NewTimer(s.timeout)
	defer t.Stop()
	select {
	case r := <-s.queue:
		return r.code, r.err
	case <-t.C:
		return "", TimeOut
	}
}

// Close stops the server.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

// Addr is the address the server listens on.
func (s *Server) Addr() string { return s.ln.Addr().String() }
