// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package config is the CLI configuration (Java io.carbynestack.cli.configuration): the virtual cloud
// setup stored in ~/.cs/config, its environment variable overrides and the interactive "configure"
// command.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/props"
)

const (
	// FolderPath and FileName make up the default configuration file ~/.cs/config.
	FolderPath = ".cs"
	FileName   = "config"
	// CommandName is the configure command (also used in "not configured" messages).
	CommandName = "configure"
	// ApplicationTitle is the program name.
	ApplicationTitle = "cs"

	primeEnv           = "CS_PRIME"
	rEnv               = "CS_R"
	rInvEnv            = "CS_R_INV"
	noSSLValidationEnv = "CS_NO_SSL_VALIDATION"
)

var (
	defaultPrime, _ = new(big.Int).SetString("198766463529478683931867765928436695041", 10)
	defaultR, _     = new(big.Int).SetString("141515903391459779531506841503331516415", 10)
	defaultRInv, _  = new(big.Int).SetString("133854242216446749056083838363708373830", 10)
)

// Error is CsCliConfigurationException.
type Error struct {
	Msg   string
	Cause error
}

func (e *Error) Error() string { return e.Msg }

// Message is the exception message.
func (e *Error) Message() string { return e.Msg }

func (e *Error) Unwrap() error { return e.Cause }

// msg is a message of the configuration bundle; like the Java code, only messages with arguments go
// through MessageFormat (which interprets quotes).
func msg(key string, args ...any) string {
	if len(args) == 0 {
		return props.Get(props.Configuration, key)
	}
	return props.Format(props.Configuration, key, args...)
}

// Configuration is the virtual cloud configuration.
type Configuration struct {
	Prime, R, RInv  *big.Int
	NoSSLValidation bool
	// TrustedCertificates are file paths.
	TrustedCertificates []string
	Providers           []*VcpConfiguration

	env *Env
}

// New returns the default configuration with two empty providers (Java new Configuration()).
func New(e *Env) *Configuration {
	return &Configuration{
		Prime: new(big.Int).Set(defaultPrime), R: new(big.Int).Set(defaultR), RInv: new(big.Int).Set(defaultRInv),
		TrustedCertificates: []string{},
		Providers:           []*VcpConfiguration{newVcp(e, 1), newVcp(e, 2)},
		env:                 e,
	}
}

// Instance is Configuration.getInstance: the configuration file, loaded once.
func (e *Env) Instance() (*Configuration, error) {
	if e.config == nil {
		c := e.LoadFromFile()
		if c == nil {
			return nil, &Error{Msg: msg("failure.not-configured", ApplicationTitle, CommandName)}
		}
		e.config = c
	}
	return e.config, nil
}

// ResetInstance drops the cached configuration (the Java tests reset the singleton by reflection).
func (e *Env) ResetInstance() { e.config = nil }

// ConfigFile returns the configuration file path, defaulting to ~/.cs/config (Configuration.getConfigFile).
func (e *Env) ConfigFile() string {
	if e.ConfigPath == "" {
		if fi, err := os.Stat(e.Home); err != nil || !fi.IsDir() {
			e.Log.Errorf(msg("read-config.failure.home-does-not-exist", e.Home))
		}
		e.ConfigPath = filepath.Join(e.Home, FolderPath, FileName)
	}
	return e.ConfigPath
}

// LoadFromFile reads the configuration file; nil if it does not exist or cannot be read or parsed.
func (e *Env) LoadFromFile() *Configuration {
	file := e.ConfigFile()
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(e.WorkDir, abs)
	}
	e.Log.Debugf("Using config file \"{}\"", abs)
	var c *Configuration
	if _, err := os.Stat(file); err == nil {
		b, err := os.ReadFile(file)
		if err != nil {
			e.Log.Errorf(msg("read-config.failure.cannot-be-read"))
		} else if c, err = e.Parse(b); err != nil {
			e.Log.Error(msg("read-config.failure.cannot-be-parsed"), err)
			c = nil
		}
	} else {
		e.Log.Debugf(msg("read-config.log.does-not-exist"))
	}
	if c == nil {
		e.Log.Debugf("Parsed configuration from file: {}", "null")
	} else {
		e.Log.Debugf("Parsed configuration from file: {}", c.String())
	}
	return c
}

// Configure asks for every setting on the console, offering the current values as defaults.
func (c *Configuration) Configure() error {
	e := c.env
	e.Printf("%s", msg("configuration.request.prime", c.GetPrime().String()))
	p, err := javautil.ParseBigInteger(e.ReadOrDefault(c.GetPrime().String()))
	if err != nil {
		return javautil.NumberFormat(err.Error())
	}
	c.Prime = p
	e.Printf("%s", msg("configuration.request.r", c.GetR().String()))
	if c.R, err = javautil.ParseBigInteger(e.ReadOrDefault(c.GetR().String())); err != nil {
		return javautil.NumberFormat(err.Error())
	}
	e.Printf("%s", msg("configuration.request.r-inv", c.GetRInv().String()))
	if c.RInv, err = javautil.ParseBigInteger(e.ReadOrDefault(c.GetRInv().String())); err != nil {
		return javautil.NumberFormat(err.Error())
	}
	yesNo := "no"
	if c.IsNoSSLValidation() {
		yesNo = "yes"
	}
	e.Printf("%s", msg("configuration.request.no-ssl-validation", yesNo))
	c.NoSSLValidation = inputToBool(e.ReadOrDefault(yesNo))
	certs := strings.Join(c.TrustedCertificates, ";")
	e.Printf("%s", msg("configuration.request.trusted-certificates", certs))
	c.TrustedCertificates = inputToList(e.ReadOrDefault(certs))
	players := max(len(c.Providers), 2)
	e.Printf("%s", msg("configuration.request.number-of-players", players))
	n, err := javautil.ParseInt(e.ReadOrDefault(strconv.Itoa(players)))
	if err != nil {
		return javautil.NumberFormat(err.Error())
	}
	if err := c.setNumberOfPlayers(n); err != nil {
		return err
	}
	for _, p := range c.Providers {
		if err := p.Configure(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Configuration) setNumberOfPlayers(n int) error {
	if n < 2 {
		return &Error{Msg: msg("configuration.invalid-input.number-of-players", n)}
	}
	ps := make([]*VcpConfiguration, n)
	copy(ps, c.Providers)
	for i := range ps {
		if ps[i] == nil {
			ps[i] = newVcp(c.env, i+1)
		}
	}
	c.Providers = ps
	return nil
}

// Provider returns provider id (1-based).
func (c *Configuration) Provider(id int) (*VcpConfiguration, error) {
	if id < 1 || id > len(c.Providers) {
		return nil, &Error{Msg: msg("configuration.access.invalid-provider-id", id)}
	}
	return c.Providers[id-1], nil
}

// GetPrime is the prime, overridden by CS_PRIME.
func (c *Configuration) GetPrime() *big.Int { return c.bigEnv(primeEnv, c.Prime) }

// GetR is R, overridden by CS_R.
func (c *Configuration) GetR() *big.Int { return c.bigEnv(rEnv, c.R) }

// GetRInv is R⁻¹, overridden by CS_R_INV.
func (c *Configuration) GetRInv() *big.Int { return c.bigEnv(rInvEnv, c.RInv) }

func (c *Configuration) bigEnv(key string, def *big.Int) *big.Int {
	if v, ok := c.env.env(key); ok {
		if b, err := javautil.ParseBigInteger(v); err == nil {
			return b
		}
	}
	return def
}

// IsNoSSLValidation is noSslValidation, overridden by CS_NO_SSL_VALIDATION (Boolean.valueOf).
func (c *Configuration) IsNoSSLValidation() bool {
	if v, ok := c.env.env(noSSLValidationEnv); ok {
		return strings.EqualFold(v, "true")
	}
	return c.NoSSLValidation
}

// WriteToFile stores the configuration (pretty printed) in the configuration file.
func (c *Configuration) WriteToFile() error {
	file := c.env.ConfigFile()
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() {
			return &Error{Msg: msg("configuration.finalize.failed.creating-config-directory")}
		}
	}
	if err := os.WriteFile(file, []byte(c.PrettyString()), 0o644); err != nil {
		return &Error{Msg: msg("configuration.finalize.failed.writing-configuration"), Cause: err}
	}
	return nil
}

// String is the Java toString.
func (c *Configuration) String() string {
	ps := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		ps[i] = p.String()
	}
	return fmt.Sprintf("Configuration{prime=%s, r=%s, rInv=%s, noSslValidation=%t, trustedCertificates=%s, providers=%s}",
		c.Prime, c.R, c.RInv, c.NoSSLValidation, javautil.ListString(c.TrustedCertificates), javautil.ListString(ps))
}

func inputToBool(in string) bool {
	switch strings.ToLower(in) {
	case "yes", "y", "true":
		return true
	}
	return false
}

func inputToList(in string) []string {
	out := []string{}
	for _, s := range strings.Split(in, ";") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ---- JSON (Jackson ObjectMapper with DefaultPrettyPrinter) ----

// PrettyString renders the configuration like Jackson's DefaultPrettyPrinter.
func (c *Configuration) PrettyString() string {
	var b strings.Builder
	b.WriteString("{\n")
	field := func(name, value string, last bool) {
		b.WriteString("  " + quote(name) + " : " + value)
		if !last {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	field("prime", c.Prime.String(), false)
	field("r", c.R.String(), false)
	field("rinv", c.RInv.String(), false)
	field("noSslValidation", strconv.FormatBool(c.NoSSLValidation), false)
	certs := make([]string, len(c.TrustedCertificates))
	for i, p := range c.TrustedCertificates {
		certs[i] = quote(pathToURI(c.env, p))
	}
	field("trustedCertificates", array(certs), false)
	b.WriteString("  \"providers\" : [")
	if len(c.Providers) == 0 {
		b.WriteString(" ]\n}")
		return b.String()
	}
	for i, p := range c.Providers {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(" {\n")
		pf := func(name, value string, last bool) {
			b.WriteString("    " + quote(name) + " : " + value)
			if !last {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		pf("id", strconv.Itoa(p.ProviderNumber), false)
		pf("baseUrl", quote(p.BaseURL), false)
		pf("amphoraServiceUrl", quote(p.AmphoraServiceURL), false)
		pf("castorServiceUrl", quote(p.CastorServiceURL), false)
		pf("ephemeralServiceUrl", quote(p.EphemeralServiceURL), false)
		pf("thymusServiceUrl", quote(p.ThymusServiceURL), false)
		pf("oauth2ClientId", quote(p.OAuth2ClientID), false)
		pf("oauth2CallbackUrl", quote(p.OAuth2CallbackURL), false)
		pf("oauth2AuthEndpointUri", quote(p.OAuth2AuthEndpointURI), false)
		pf("oauth2TokenEndpointUri", quote(p.OAuth2TokenEndpointURI), true)
		b.WriteString("  }")
	}
	b.WriteString(" ]\n}")
	return b.String()
}

func array(items []string) string {
	if len(items) == 0 {
		return "[ ]"
	}
	return "[ " + strings.Join(items, ", ") + " ]"
}

// quote is a Jackson JSON string: '"' and '\' escaped, control characters as \n, \t, ... or \u00XX.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pathToURI is Path.toUri().toString() as Jackson serializes a java.nio.file.Path: an absolute file
// URI, with a trailing "/" for existing directories.
func pathToURI(e *Env, p string) string {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(e.WorkDir, abs)
	}
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() && !strings.HasSuffix(abs, "/") {
		abs += "/"
	}
	var b strings.Builder
	b.WriteString("file://")
	for _, c := range []byte(abs) {
		if isURIPathChar(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func isURIPathChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("/-_.!~*'()@:&=+$,;", c) >= 0
}

// uriToPath is Jackson's Path deserializer: a string without ':' is a path, otherwise a file URI.
func uriToPath(s string) (string, error) {
	if !strings.Contains(s, ":") {
		return s, nil
	}
	if !strings.HasPrefix(s, "file:") {
		return "", fmt.Errorf("Provider \"%s\" not installed", strings.SplitN(s, ":", 2)[0])
	}
	rest := strings.TrimPrefix(s, "file:")
	rest = strings.TrimPrefix(rest, "//")
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		if rest[i] == '%' && i+2 < len(rest) {
			if v, err := strconv.ParseUint(rest[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(rest[i])
	}
	p := b.String()
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p, nil
}

// Parse reads a configuration document (Jackson: unknown properties are rejected, missing ones keep
// their defaults, numbers may also be given as strings).
func (e *Env) Parse(b []byte) (*Configuration, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("No content to map due to end-of-input")
	}
	var raw map[string]json.RawMessage
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return nil, err
	}
	c := New(e)
	for k, v := range raw {
		var err error
		switch k {
		case "prime":
			c.Prime, err = bigValue(v)
		case "r":
			c.R, err = bigValue(v)
		case "rinv":
			c.RInv, err = bigValue(v)
		case "noSslValidation":
			err = json.Unmarshal(v, &c.NoSSLValidation)
		case "trustedCertificates":
			var certs []string
			if err = json.Unmarshal(v, &certs); err == nil {
				c.TrustedCertificates = []string{}
				for _, s := range certs {
					p, perr := uriToPath(s)
					if perr != nil {
						return nil, perr
					}
					c.TrustedCertificates = append(c.TrustedCertificates, p)
				}
			}
		case "providers":
			var ps []map[string]json.RawMessage
			if err = json.Unmarshal(v, &ps); err == nil {
				c.Providers = make([]*VcpConfiguration, len(ps))
				for i, p := range ps {
					if c.Providers[i], err = e.parseVcp(p); err != nil {
						return nil, err
					}
				}
			}
		default:
			err = fmt.Errorf("Unrecognized field \"%s\" (class io.carbynestack.cli.configuration.Configuration), not marked as ignorable", k)
		}
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func bigValue(v json.RawMessage) (*big.Int, error) {
	s := strings.TrimSpace(string(v))
	if strings.HasPrefix(s, "\"") {
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, err
		}
	}
	b, err := javautil.ParseBigInteger(s)
	if err != nil {
		return nil, fmt.Errorf("Cannot deserialize value of type `java.math.BigInteger` from %s", string(v))
	}
	return b, nil
}
