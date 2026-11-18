// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	amphora "github.com/Samrat7745/amphora/go/client"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// AmphoraClientCliIT: get-secrets against two HTTPS Amphora stubs (WireMock in Java) whose addresses
// come from CS_VCP_<n>_AMPHORA_URL, with the real Amphora client and the configuration's TLS settings.

type amphoraIT struct {
	tc         *testutil.TempConfig
	list       []amphora.Metadata
	certA      testutil.Cert
	certB      testutil.Cert
	servers    []*httptest.Server
	configured func(*config.Configuration)
}

func newAmphoraIT(t *testing.T) *amphoraIT {
	it := &amphoraIT{list: metadataList(), certA: testutil.NewCert(t), certB: testutil.NewCert(t)}
	page, _ := json.Marshal(amphora.MetadataPage{Content: it.list, Number: 1, Size: 3, TotalElements: 3, TotalPages: 1})
	vars := map[string]string{}
	for i := 0; i < 2; i++ {
		s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/secret-shares") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(page)
		}))
		// both stubs use keystore A (Java: keyStoreA.jks for both WireMock rules)
		s.TLS = &tls.Config{Certificates: []tls.Certificate{it.certA.TLS}}
		s.StartTLS()
		t.Cleanup(s.Close)
		it.servers = append(it.servers, s)
		vars[[]string{"CS_VCP_1_AMPHORA_URL", "CS_VCP_2_AMPHORA_URL"}[i]] = strings.Replace(s.URL, "127.0.0.1", "localhost", 1)
	}
	it.tc = testutil.NewTempConfig(t, "", vars, func(e *config.Env) *config.Configuration {
		return testutil.Configuration(e, vars["CS_VCP_1_AMPHORA_URL"], vars["CS_VCP_2_AMPHORA_URL"])
	})
	return it
}

func (it *amphoraIT) update(t *testing.T, f func(c *config.Configuration)) {
	f(it.tc.Config)
	it.tc.Write(t)
}

// cli is getCliWithTempConfigAndArgs: the real client, "--config-file" passed to the client CLI.
func (it *amphoraIT) cli(args ...string) *ClientCli {
	return NewAmphoraCli(it.tc.Env, append([]string{"--config-file", it.tc.ConfigFile}, args...), false, nil)
}

func assertUntrusted(t *testing.T, err error) {
	t.Helper()
	re := asRunner(t, err)
	if !strings.Contains(re.Msg, "Fetching secret metadata failed") {
		t.Fatal(re.Msg)
	}
	var ce *amphora.ClientError
	if !asErr(re.Cause, &ce) || !strings.Contains(ce.Cause.Error(), "certificate") {
		t.Fatalf("expected a certificate verification failure, got %v", re.Cause)
	}
}

func asErr(err error, target **amphora.ClientError) bool {
	ce, ok := err.(*amphora.ClientError)
	if ok {
		*target = ce
	}
	return ok
}

func TestAmphoraIT_insecureConnection(t *testing.T) {
	it := newAmphoraIT(t)
	assertUntrusted(t, it.cli(ListSecretsCommand).Parse())
}

func TestAmphoraIT_disableSslValidation(t *testing.T) {
	it := newAmphoraIT(t)
	it.update(t, func(c *config.Configuration) { c.NoSSLValidation = true })
	if err := it.cli(ListSecretsCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.tc.Out.String(), MetadataListToString(it.list, false)) {
		t.Fatal(it.tc.Out.String())
	}
}

func TestAmphoraIT_trustGivenCertificate(t *testing.T) {
	it := newAmphoraIT(t)
	it.update(t, func(c *config.Configuration) { c.TrustedCertificates = []string{it.certA.PEMFile} })
	if err := it.cli(ListSecretsCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.tc.Out.String(), MetadataListToString(it.list, false)) {
		t.Fatal(it.tc.Out.String())
	}
}

func TestAmphoraIT_wrongTrustedCertificate(t *testing.T) {
	it := newAmphoraIT(t)
	it.update(t, func(c *config.Configuration) { c.TrustedCertificates = []string{it.certB.PEMFile} })
	assertUntrusted(t, it.cli(ListSecretsCommand).Parse())
}
