// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"io"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/logx"
	"github.com/Samrat7745/cli/go/internal/props"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

func msg(key string, args ...any) string {
	if len(args) == 0 {
		return props.Get(props.Configuration, key)
	}
	return props.Format(props.Configuration, key, args...)
}

// ---- ConfigurationCommandTest ----

func commandEnv(t *testing.T, stdin string) (*config.Env, *testutil.SyncBuffer, string) {
	e, out := testutil.Env(t, stdin, nil)
	e.Log = logx.New(io.Discard)
	f := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.ConfigPath = f
	return e, out, f
}

func TestConfigurationCommand_givenUserDiscards_whenFinishConfiguration_thenDontWriteConfig(t *testing.T) {
	e, _, f := commandEnv(t, "n\n")
	if err := (&config.Command{}).Finalize(e, config.New(e)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); len(b) != 0 {
		t.Fatalf("%q", b)
	}
}

func TestConfigurationCommand_givenUserConfirms_whenFinishConfiguration_thenWriteConfig(t *testing.T) {
	e, _, f := commandEnv(t, "y\n")
	c := testutil.DefaultConfiguration(e)
	c.Prime = big.NewInt(rand.Int63())
	c.NoSSLValidation = true
	if err := (&config.Command{}).Finalize(e, c); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != c.PrettyString() {
		t.Fatalf("%s", b)
	}
}

func TestConfigurationCommand_givenRepeatedInvalidInput_whenFinishConfiguration_thenThrowException(t *testing.T) {
	e, out, f := commandEnv(t, "invalid\ninvalid\ninvalid\ninvalid\n")
	c := testutil.DefaultConfiguration(e)
	fi, _ := os.Stat(f)
	err := (&config.Command{}).Finalize(e, c)
	if err == nil {
		t.Fatal("Exception expected")
	}
	if fi2, _ := os.Stat(f); !fi2.ModTime().Equal(fi.ModTime()) {
		t.Fatal("configuration file was written")
	}
	if !strings.Contains(out.String(), msg("configuration.finalize.invalid-input.request-complete", "yes")) {
		t.Fatal(out.String())
	}
	if !strings.Contains(err.Error(), msg("configuration.finalize.failed.invalid-input-request-complete", "invalid")) {
		t.Fatal(err)
	}
}

func TestConfigurationCommand_givenConfigurationFilePathUndefined_whenPerformingConfiguration_logDebugMessage(t *testing.T) {
	e, _, _ := commandEnv(t, "")
	e.ConfigPath = filepath.Join(t.TempDir(), "missing")
	// The Java test sees this DEBUG message only when debug logging is enabled by an earlier test.
	e.Log.SetLevel(logx.Debug)
	if err := (&config.Command{}).Configure(e); err == nil {
		t.Fatal("Exception expected")
	}
	if !strings.Contains(e.Log.Log(), msg("read-config.log.fallback")) {
		t.Fatal(e.Log.Log())
	}
}

// Beyond the Java suite: a full interactive configuration round trip.
func TestConfigurationCommand_interactiveConfigurationIsStored(t *testing.T) {
	in := strings.Join([]string{
		"", "", "", "yes", "", "2",
		"https://a.example.com/", "", "", "", "", "client-a", "https://a.example.com/oauth2/auth", "https://a.example.com/oauth2/token", "http://127.0.0.1:32768/callback",
		"https://b.example.com/", "", "", "", "", "client-b", "", "", "http://127.0.0.1:32768/callback",
		"yes"}, "\n") + "\n"
	e, _, f := commandEnv(t, in)
	e.ConfigPath = filepath.Join(t.TempDir(), ".cs", "config")
	f = e.ConfigPath
	if err := (&config.Command{}).Configure(e); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	c, err := e.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	p1, p2 := c.Providers[0], c.Providers[1]
	if !c.NoSSLValidation || p1.AmphoraServiceURL != "https://a.example.com/amphora" || p1.CastorServiceURL != "https://a.example.com/castor" ||
		p1.EphemeralServiceURL != "https://a.example.com/" || p1.ThymusServiceURL != "https://a.example.com/thymus" ||
		p1.OAuth2ClientID != "client-a" || p2.BaseURL != "https://b.example.com/" || p2.OAuth2ClientID != "client-b" {
		t.Fatal(c)
	}
}

// ---- VcpConfigurationTest ----

func TestVcpConfiguration_givenEnvVariableForVcp1Services_whenGetUrl_thenReturnEnvVariableValue(t *testing.T) {
	amphoraURL := "http://" + uuid.NewString()[:15]
	castorURL := "http://" + uuid.NewString()[:15]
	ephemeralURL := "http://" + uuid.NewString()[:15]
	e, _ := testutil.Env(t, "", map[string]string{
		props.MessageFormat(config.AmphoraURLEnvKeyFormat, 1):   amphoraURL,
		props.MessageFormat(config.CastorURLEnvKeyFormat, 1):    castorURL,
		props.MessageFormat(config.EphemeralURLEnvKeyFormat, 1): ephemeralURL,
	})
	c := testutil.DefaultConfiguration(e)
	p, _ := c.Provider(1)
	if p.AmphoraURL() != amphoraURL || p.CastorURL() != castorURL || p.EphemeralURL() != ephemeralURL {
		t.Fatal(p.AmphoraURL(), p.CastorURL(), p.EphemeralURL())
	}
	p2, _ := c.Provider(2)
	if p2.AmphoraURL() != testutil.VCP2BaseURL+"/amphora" {
		t.Fatal("provider 2 must not be overridden")
	}
}

func TestVcpConfiguration_givenNewVcpConfigurationAndBaseUrlNotProvided_whenPromptServiceUrls_thenNoDefaultProvided(t *testing.T) {
	amphoraURL := "http://a" + uuid.NewString()[:8]
	castorURL := "http://c" + uuid.NewString()[:8]
	ephemeralURL := "http://e" + uuid.NewString()[:8]
	thymusURL := "http://t" + uuid.NewString()[:8]
	clientID := uuid.NewString()
	callback := "http://localhost/vcp-1"
	authURI := "http://x" + uuid.NewString()[:8] + "/oauth2/auth"
	tokenURI := "http://x" + uuid.NewString()[:8] + "/oauth2/token"
	in := strings.Join([]string{"", amphoraURL, castorURL, ephemeralURL, thymusURL, clientID, authURI, tokenURI, callback}, "\n") + "\n"
	e, out := testutil.Env(t, in, nil)
	v := config.NewVcp(e, 1)
	if err := v.Configure(); err != nil {
		t.Fatal(err)
	}
	if v.BaseURL != "" {
		t.Fatal("base URL must stay unset")
	}
	for _, key := range []string{"amphora-service-url", "castor-service-url", "ephemeral-service-url", "thymus-service-url",
		"oauth2-client-id", "oauth2-callback-url"} {
		if !strings.Contains(out.String(), msg("configuration.request.vcp."+key, "")) {
			t.Fatalf("prompt %s without default missing:\n%s", key, out.String())
		}
	}
	if v.AmphoraURL() != amphoraURL || v.CastorURL() != castorURL || v.EphemeralURL() != ephemeralURL ||
		v.ThymusURL() != thymusURL || v.ClientID() != clientID || v.CallbackURL() != callback {
		t.Fatal(v)
	}
}

func TestVcpConfiguration_givenExistingVcpConfigurationAndNewBaseUrlProvided_whenConfigure_thenProvideNewDefaults(t *testing.T) {
	base := "http://" + strings.ReplaceAll(uuid.NewString()[:15], "-", "x") + "/"
	e, _ := testutil.Env(t, base+"\n\n\n\n", nil)
	e.Log = logx.New(io.Discard)
	v, _ := testutil.DefaultConfiguration(e).Provider(1)
	if err := v.Configure(); err != nil {
		t.Fatal(err)
	}
	castor, _ := config.CastorRestURI(props.MessageFormat(config.CastorURLFormat, base))
	if v.BaseURL != base || v.AmphoraURL() != props.MessageFormat(config.AmphoraURLFormat, base) ||
		v.CastorURL() != castor || v.EphemeralURL() != props.MessageFormat(config.EphemeralURLFormat, base) {
		t.Fatal(v)
	}
}

// Beyond the Java suite: the configuration file format is Jackson's (checked against the Java CLI).
func TestConfiguration_prettyStringAndParseRoundTrip(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	c := testutil.DefaultConfiguration(e)
	c.TrustedCertificates = []string{"/tmp/a b.pem"}
	s := c.PrettyString()
	if !strings.Contains(s, `"trustedCertificates" : [ "file:///tmp/a%20b.pem" ],`) || !strings.HasPrefix(s, "{\n  \"prime\" : 198766463529478683931867765928436695041,\n") {
		t.Fatal(s)
	}
	back, err := e.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if back.PrettyString() != s {
		t.Fatalf("%s\n---\n%s", back.PrettyString(), s)
	}
	if _, err := e.Parse([]byte(`{"unknown": 1}`)); err == nil {
		t.Fatal("unknown properties must be rejected")
	}
}
