// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/Samrat7745/cli/go/internal/props"
)

// Environment variable formats of the per-provider overrides ({0} is the provider number).
const (
	AmphoraURLEnvKeyFormat        = "CS_VCP_{0}_AMPHORA_URL"
	CastorURLEnvKeyFormat         = "CS_VCP_{0}_CASTOR_URL"
	EphemeralURLEnvKeyFormat      = "CS_VCP_{0}_EPHEMERAL_URL"
	ThymusURLEnvKeyFormat         = "CS_VCP_{0}_THYMUS_URL"
	OAuth2AuthEndpointURIEnv      = "CS_VCP_{0}_OAUTH2_AUTH_ENDPOINT_URI"
	OAuth2TokenEndpointURIEnv     = "CS_VCP_{0}_OAUTH2_TOKEN_ENDPOINT_URI"
	OAuth2ClientIDEnvKeyFormat    = "CS_VCP_{0}_OAUTH2_CLIENT_ID"
	OAuth2CallbackURLEnvKeyFormat = "CS_VCP_{0}_OAUTH2_CALLBACK_URL"

	// Default service URLs derived from a new base URL ({0} is the base URL).
	AmphoraURLFormat   = "{0}amphora"
	CastorURLFormat    = "{0}castor"
	EphemeralURLFormat = "{0}"
	ThymusURLFormat    = "{0}thymus"
)

// VcpConfiguration is the configuration of one virtual cloud provider. URIs are kept as strings;
// "" is an unset (null) URI.
type VcpConfiguration struct {
	ProviderNumber int
	BaseURL        string
	// AmphoraServiceURL is AmphoraServiceUri.getServiceUri() (the address as given).
	AmphoraServiceURL string
	// CastorServiceURL is CastorServiceUri.getRestServiceUri() (scheme http or https).
	CastorServiceURL       string
	EphemeralServiceURL    string
	ThymusServiceURL       string
	OAuth2ClientID         string
	OAuth2AuthEndpointURI  string
	OAuth2TokenEndpointURI string
	OAuth2CallbackURL      string

	env *Env
}

func newVcp(e *Env, n int) *VcpConfiguration { return &VcpConfiguration{ProviderNumber: n, env: e} }

// NewVcp returns an empty provider configuration (Java new VcpConfiguration(n)).
func NewVcp(e *Env, n int) *VcpConfiguration { return newVcp(e, n) }

func (v *VcpConfiguration) override(format, def string) string {
	if s, ok := v.env.env(props.MessageFormat(format, v.ProviderNumber)); ok {
		return s
	}
	return def
}

// AmphoraURL is the Amphora service URL, overridden by CS_VCP_<n>_AMPHORA_URL.
func (v *VcpConfiguration) AmphoraURL() string {
	return v.override(AmphoraURLEnvKeyFormat, v.AmphoraServiceURL)
}

// CastorURL is the Castor REST service URL, overridden by CS_VCP_<n>_CASTOR_URL.
func (v *VcpConfiguration) CastorURL() string {
	if s, ok := v.env.env(props.MessageFormat(CastorURLEnvKeyFormat, v.ProviderNumber)); ok {
		if r, err := CastorRestURI(s); err == nil {
			return r
		}
		return s
	}
	return v.CastorServiceURL
}

// EphemeralURL is the Ephemeral service URL, overridden by CS_VCP_<n>_EPHEMERAL_URL.
func (v *VcpConfiguration) EphemeralURL() string {
	return v.override(EphemeralURLEnvKeyFormat, v.EphemeralServiceURL)
}

// ThymusURL is the Thymus service URL, overridden by CS_VCP_<n>_THYMUS_URL.
func (v *VcpConfiguration) ThymusURL() string {
	return v.override(ThymusURLEnvKeyFormat, v.ThymusServiceURL)
}

// ClientID is the OAuth2 client id, overridden by CS_VCP_<n>_OAUTH2_CLIENT_ID.
func (v *VcpConfiguration) ClientID() string {
	return v.override(OAuth2ClientIDEnvKeyFormat, v.OAuth2ClientID)
}

// CallbackURL is the OAuth2 callback URL, overridden by CS_VCP_<n>_OAUTH2_CALLBACK_URL.
func (v *VcpConfiguration) CallbackURL() string {
	return v.override(OAuth2CallbackURLEnvKeyFormat, v.OAuth2CallbackURL)
}

// AuthEndpointURI is the OAuth2 authorization endpoint, overridden by CS_VCP_<n>_OAUTH2_AUTH_ENDPOINT_URI.
func (v *VcpConfiguration) AuthEndpointURI() string {
	return v.override(OAuth2AuthEndpointURIEnv, v.OAuth2AuthEndpointURI)
}

// TokenEndpointURI is the OAuth2 token endpoint, overridden by CS_VCP_<n>_OAUTH2_TOKEN_ENDPOINT_URI.
func (v *VcpConfiguration) TokenEndpointURI() string {
	return v.override(OAuth2TokenEndpointURIEnv, v.OAuth2TokenEndpointURI)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Configure asks for the provider's settings. A changed base URL resets the service URLs to the
// defaults derived from it.
func (v *VcpConfiguration) Configure() error {
	e := v.env
	failed := func(err error) error { return &Error{Msg: msg("configuration.failed"), Cause: err} }
	ask := func(key string, shown any) {
		e.Printf("\t%s", msg(key, shown))
	}
	e.Printf("Configuring Provider #%d\n", v.ProviderNumber)
	ask("configuration.request.vcp.base-url", v.BaseURL)
	old := v.BaseURL
	if nv := e.ReadOrDefault(old); nv != old {
		if err := checkURI(nv); err != nil {
			return failed(err)
		}
		v.BaseURL = nv
		v.AmphoraServiceURL = props.MessageFormat(AmphoraURLFormat, nv)
		c, err := CastorRestURI(props.MessageFormat(CastorURLFormat, nv))
		if err != nil {
			return failed(err)
		}
		v.CastorServiceURL = c
		v.EphemeralServiceURL = props.MessageFormat(EphemeralURLFormat, nv)
		v.ThymusServiceURL = props.MessageFormat(ThymusURLFormat, nv)
	}
	ask("configuration.request.vcp.amphora-service-url", v.AmphoraServiceURL)
	a := e.ReadOrDefault(v.AmphoraServiceURL)
	if err := AmphoraServiceURI(a); err != nil {
		return failed(err)
	}
	v.AmphoraServiceURL = a
	ask("configuration.request.vcp.castor-service-url", v.CastorServiceURL)
	c, err := CastorRestURI(e.ReadOrDefault(v.CastorServiceURL))
	if err != nil {
		return failed(err)
	}
	v.CastorServiceURL = c
	for _, f := range []struct {
		key    string
		shown  any
		target *string
	}{
		{"configuration.request.vcp.ephemeral-service-url", v.EphemeralServiceURL, &v.EphemeralServiceURL},
		{"configuration.request.vcp.thymus-service-url", v.ThymusServiceURL, &v.ThymusServiceURL},
	} {
		ask(f.key, f.shown)
		s := e.ReadOrDefault(*f.target)
		if err := checkURI(s); err != nil {
			return failed(err)
		}
		*f.target = s
	}
	ask("configuration.request.vcp.oauth2-client-id", v.OAuth2ClientID)
	v.OAuth2ClientID = e.ReadOrDefault(v.OAuth2ClientID)
	// The Java CLI shows the (overridden) authorization endpoint as default of both endpoint prompts.
	ask("configuration.request.vcp.oauth2-auth-endpoint-uri", nullable(v.AuthEndpointURI()))
	if v.OAuth2AuthEndpointURI, err = readURI(e, v.OAuth2AuthEndpointURI); err != nil {
		return failed(err)
	}
	ask("configuration.request.vcp.oauth2-token-endpoint-uri", nullable(v.AuthEndpointURI()))
	if v.OAuth2TokenEndpointURI, err = readURI(e, v.OAuth2TokenEndpointURI); err != nil {
		return failed(err)
	}
	ask("configuration.request.vcp.oauth2-callback-url", v.OAuth2CallbackURL)
	if v.OAuth2CallbackURL, err = readURI(e, v.OAuth2CallbackURL); err != nil {
		return failed(err)
	}
	return nil
}

func readURI(e *Env, def string) (string, error) {
	s := e.ReadOrDefault(def)
	return s, checkURI(s)
}

// checkURI rejects strings java.net.URI.create rejects (illegal characters such as spaces).
func checkURI(s string) error {
	if strings.ContainsAny(s, " \"<>\\^`{|}") {
		return fmt.Errorf("Illegal character in URI: %s", s)
	}
	if _, err := url.Parse(s); err != nil {
		return err
	}
	return nil
}

var hostLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)

// javaHost is java.net.URI.getHost(): an IPv4/IPv6 literal or a host name whose last label starts
// with a letter; otherwise the authority is registry based and the host is null ("").
func javaHost(u *url.URL) string {
	h := u.Hostname()
	if h == "" {
		return ""
	}
	if ip := net.ParseIP(h); ip != nil {
		return h
	}
	labels := strings.Split(strings.TrimSuffix(h, "."), ".")
	for _, l := range labels {
		if !hostLabel.MatchString(l) {
			return ""
		}
	}
	last := labels[len(labels)-1]
	if c := last[0]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return ""
	}
	return h
}

// AmphoraServiceURI validates an Amphora service address like new AmphoraServiceUri(address).
func AmphoraServiceURI(addr string) error {
	if addr == "" {
		return errors.New("serviceAddress must not be empty!")
	}
	if err := checkURI(addr); err != nil {
		return fmt.Errorf("Unable to construct AmphoraServiceUri: %w", err)
	}
	u, _ := url.Parse(addr)
	if javaHost(u) == "" {
		return errors.New("Invalid service address.")
	}
	return nil
}

// CastorRestURI is new CastorServiceUri(address).getRestServiceUri(): the address with scheme https
// (for https and wss) or http.
func CastorRestURI(addr string) (string, error) {
	if addr == "" {
		return "", errors.New("serviceAddress must not be empty!")
	}
	if err := checkURI(addr); err != nil {
		return "", fmt.Errorf("Unable to construct CastorServiceUri: %w", err)
	}
	u, _ := url.Parse(addr)
	if javaHost(u) == "" {
		return "", errors.New("Invalid service address.")
	}
	scheme := "http"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "https"
	}
	if i := strings.Index(addr, "://"); i >= 0 {
		return scheme + addr[i:], nil
	}
	return scheme + ":" + strings.TrimPrefix(addr, u.Scheme+":"), nil
}

// String is the Java toString.
func (v *VcpConfiguration) String() string {
	n := func(s string) string {
		if s == "" {
			return "null"
		}
		return s
	}
	return fmt.Sprintf("VcpConfiguration{providerNumber=%d, baseUrl=%s, amphoraServiceUrl=%s, castorServiceUrl=%s, "+
		"ephemeralServiceUrl=%s, thymusServiceUrl=%s, oauth2ClientId=%s, oauth2AuthEndpointUri=%s, "+
		"oauth2TokenEndpointUri=%s, oauth2CallbackUrl=%s}", v.ProviderNumber, n(v.BaseURL), n(v.AmphoraServiceURL),
		n(v.CastorServiceURL), n(v.EphemeralServiceURL), n(v.ThymusServiceURL), n(v.OAuth2ClientID),
		n(v.OAuth2AuthEndpointURI), n(v.OAuth2TokenEndpointURI), n(v.OAuth2CallbackURL))
}

func (e *Env) parseVcp(raw map[string]json.RawMessage) (*VcpConfiguration, error) {
	v := newVcp(e, 0)
	for k, val := range raw {
		if k == "id" {
			if err := json.Unmarshal(val, &v.ProviderNumber); err != nil {
				return nil, err
			}
			continue
		}
		var s *string
		if err := json.Unmarshal(val, &s); err != nil {
			return nil, err
		}
		if s == nil {
			return nil, fmt.Errorf("null value for \"%s\"", k)
		}
		var err error
		switch k {
		case "baseUrl":
			v.BaseURL, err = *s, checkURI(*s)
		case "amphoraServiceUrl":
			v.AmphoraServiceURL, err = *s, AmphoraServiceURI(*s)
		case "castorServiceUrl":
			v.CastorServiceURL, err = CastorRestURI(*s)
		case "ephemeralServiceUrl":
			v.EphemeralServiceURL, err = *s, checkURI(*s)
		case "thymusServiceUrl":
			v.ThymusServiceURL, err = *s, checkURI(*s)
		case "oauth2ClientId":
			v.OAuth2ClientID = *s
		case "oauth2CallbackUrl":
			v.OAuth2CallbackURL, err = *s, checkURI(*s)
		case "oauth2AuthEndpointUri":
			v.OAuth2AuthEndpointURI, err = *s, checkURI(*s)
		case "oauth2TokenEndpointUri":
			v.OAuth2TokenEndpointURI, err = *s, checkURI(*s)
		default:
			err = fmt.Errorf("Unrecognized field \"%s\" (class io.carbynestack.cli.configuration.VcpConfiguration), not marked as ignorable", k)
		}
		if err != nil {
			return nil, err
		}
	}
	return v, nil
}
