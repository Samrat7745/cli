// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package testutil holds the test fixtures of the Java test suite: TemporaryConfiguration,
// ConfigurationUtil and TokenUtils, plus a self-signed TLS certificate helper.
package testutil

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/login"
	"github.com/Samrat7745/cli/go/internal/logx"
)

// Base URLs of ConfigurationUtil.
const (
	VCP1BaseURL = "https://apollo.example.com"
	VCP2BaseURL = "https://starbuck.example.com"
)

// Resources is the Java test resource directory (src/test/resources).
func Resources(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "src", "test", "resources", name)
}

// SyncBuffer is a goroutine safe output buffer (SystemOutRule).
type SyncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// String returns everything written.
func (s *SyncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Env returns an environment with captured output, the given stdin and the given environment
// variables only (EnvironmentVariables rule), in a temporary home directory.
func Env(t *testing.T, stdin string, vars map[string]string) (*config.Env, *SyncBuffer) {
	t.Helper()
	out := &SyncBuffer{}
	home := t.TempDir()
	return &config.Env{
		In:  bufio.NewReader(strings.NewReader(stdin)),
		Out: out,
		Log: logx.New(out),
		Getenv: func(k string) (string, bool) {
			v, ok := vars[k]
			return v, ok
		},
		Now:     time.Now,
		Home:    home,
		WorkDir: home,
	}, out
}

// Configuration is ConfigurationUtil.getConfiguration(vcp1, vcp2): two providers whose services live
// below the given URLs (base URLs stay apollo/starbuck.example.com).
func Configuration(e *config.Env, vcp1, vcp2 string) *config.Configuration {
	c := config.New(e)
	for i, base := range []string{vcp1, vcp2} {
		p := c.Providers[i]
		p.BaseURL = []string{VCP1BaseURL, VCP2BaseURL}[i]
		p.AmphoraServiceURL = base + "/amphora"
		p.CastorServiceURL, _ = config.CastorRestURI(base + "/castor")
		p.ThymusServiceURL = base + "/iam/policies"
		p.EphemeralServiceURL = base
		p.OAuth2ClientID = uuid.NewString()
		p.OAuth2CallbackURL = fmt.Sprintf("http://localhost/vcp-%d", i+1)
		p.OAuth2AuthEndpointURI = base + "/auth2/auth"
		p.OAuth2TokenEndpointURI = base + "/auth2/token"
	}
	return c
}

// DefaultConfiguration is ConfigurationUtil.getConfiguration().
func DefaultConfiguration(e *config.Env) *config.Configuration {
	return Configuration(e, VCP1BaseURL, VCP2BaseURL)
}

// OIDCTokens is TokenUtils.createToken: the token response in src/test/resources/id_token.json.
func OIDCTokens(t *testing.T) login.OIDCTokens {
	t.Helper()
	b, err := os.ReadFile(Resources("id_token.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tok login.OIDCTokens
	if err := json.Unmarshal(b, &tok); err != nil {
		t.Fatal(err)
	}
	return tok
}

// TempConfig is TemporaryConfiguration: a configuration file and a token store (tokens from
// id_token.json, valid for two hours) in a temporary directory, set as the Env's locations.
type TempConfig struct {
	Env        *config.Env
	Out        *SyncBuffer
	Config     *config.Configuration
	ConfigFile string
	TokenFile  string
}

// NewTempConfig creates the files from cfg (nil: DefaultConfiguration).
func NewTempConfig(t *testing.T, stdin string, vars map[string]string,
	cfg func(e *config.Env) *config.Configuration) *TempConfig {
	t.Helper()
	e, out := Env(t, stdin, vars)
	if cfg == nil {
		cfg = DefaultConfiguration
	}
	tc := &TempConfig{Env: e, Out: out, Config: cfg(e)}
	dir := t.TempDir()
	tc.ConfigFile = filepath.Join(dir, "config")
	tc.TokenFile = filepath.Join(dir, "access-tokens.json")
	e.ConfigPath = tc.ConfigFile
	e.TokenPath = tc.TokenFile
	tc.Write(t)
	tok := OIDCTokens(t)
	var tokens []login.Token
	for _, p := range tc.Config.Providers {
		vt, err := login.TokenFrom(time.Now(), p.BaseURL, tok)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, vt)
	}
	if err := (&login.Store{Tokens: tokens}).Persist(e); err != nil {
		t.Fatal(err)
	}
	return tc
}

// Write stores the configuration (applyConfigUpdate).
func (tc *TempConfig) Write(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(tc.ConfigFile, []byte(tc.Config.PrettyString()), 0o644); err != nil {
		t.Fatal(err)
	}
	tc.Env.ResetInstance()
}

// Cert is a self-signed certificate for localhost and 127.0.0.1.
type Cert struct {
	TLS     tls.Certificate
	PEMFile string
}

// NewCert creates a certificate and writes its PEM to a temporary file. It replaces the Java tests'
// certA.pem/keyStoreA.jks, which have no subject alternative name and expired in 2018 (Java trusts a
// pinned anchor regardless; Go verifies the leaf).
func NewCert(t *testing.T) Cert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(f, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	return Cert{TLS: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, PEMFile: f}
}
