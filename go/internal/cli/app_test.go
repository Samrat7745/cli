// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/login"
	"github.com/Samrat7745/cli/go/internal/logx"
	"github.com/Samrat7745/cli/go/internal/props"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// app is getCsCliApplication: the application with the amphora client and a mocked or real
// configure command. The logger writes to its own buffer (log4j's console appender is not part of
// the captured System.out in the Java tests).
func app(t *testing.T, tc *testutil.TempConfig, mockedConfigure bool) (*App, *int) {
	t.Helper()
	tc.Env.Log = logx.New(io.Discard)
	a := &App{Env: tc.Env, Configure: &config.Command{}, Login: nil, Clients: []Client{AmphoraClient(nil)}}
	a.Login = login.NewCommand()
	calls := 0
	if mockedConfigure {
		a.Configure.Run = func(*config.Env) error { calls++; return nil }
	}
	return a, &calls
}

func TestApp_showHelp(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	if err := a.Run("--help"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tc.Out.String(), "Usage: "+config.ApplicationTitle) {
		t.Fatal(tc.Out.String())
	}
}

func TestApp_setDebug(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	if _, ok := a.Run("--debug").(*jc.ParameterError); !ok {
		t.Fatal("expected ParameterException")
	}
	if !tc.Env.Log.IsDebugEnabled() {
		t.Fatal("debug not enabled")
	}
}

func TestApp_setConfigFile(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, calls := app(t, tc, true)
	if err := a.Run("configure"); err != nil || *calls != 1 {
		t.Fatal(err, *calls)
	}
}

func TestApp_invalidCommand(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	pe, ok := a.Run("invalidCommand").(*jc.ParameterError)
	if !ok || !pe.MissingCommand || pe.Msg != "Expected a command, got invalidCommand" {
		t.Fatal(pe)
	}
}

func TestApp_detailedConfigurationHelp(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, false)
	if err := a.Run("configure", "--help"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tc.Out.String(), props.UnescapeJava(props.Get(props.Configuration, "detailed-help"))) {
		t.Fatal(tc.Out.String())
	}
}

func TestApp_printsConfigurationInUse(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, false)
	if err := a.Run("--config-file", tc.ConfigFile, "configure", "show"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tc.Out.String(), tc.Config.PrettyString()) {
		t.Fatalf("%s\n---\n%s", tc.Out.String(), tc.Config.PrettyString())
	}
}

func TestApp_commandExecution(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	ce, ok := a.Run(AmphoraClientName, "some", "dummy", "args").(*CliError)
	if !ok || !strings.HasPrefix(ce.Help, "Usage: cs amphora") {
		t.Fatal(ce)
	}
}

func TestApp_mainInvalidCommand(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	if code := a.Main(io.Discard, "--config-file", `\/\some invalid %& Path!°/`); code != 1 {
		t.Fatal(code)
	}
	if !strings.HasPrefix(tc.Env.Log.Log(), "No command defined") {
		t.Fatal(tc.Env.Log.Log())
	}
	if !strings.HasPrefix(tc.Out.String(), "Usage: cs") {
		t.Fatal(tc.Out.String())
	}
}

func TestApp_mainUnexpectedError(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	if code := a.Main(io.Discard, AmphoraClientName); code != 2 {
		t.Fatal(code)
	}
	if log := tc.Env.Log.Log(); !strings.HasPrefix(log, "Unexpected error occurred") || !strings.Contains(log, "Usage: cs amphora") {
		t.Fatal(log)
	}
}

func TestApp_mainCommandExecutionError(t *testing.T) {
	// The services refuse connections (127.0.0.1:1) instead of resolving apollo.example.com, so the
	// test needs no network.
	tc := testutil.NewTempConfig(t, "", nil, func(e *config.Env) *config.Configuration {
		return testutil.Configuration(e, "http://127.0.0.1:1", "http://127.0.0.1:2")
	})
	a, _ := app(t, tc, true)
	if code := a.Main(io.Discard, "--config-file", tc.ConfigFile, AmphoraClientName, GetSecretCommand, uuid.NewString()); code != 3 {
		t.Fatal(code, tc.Env.Log.Log())
	}
}

func TestApp_mainInvalidConfiguration(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	a.Configure.Run = nil
	// The Java test sees this DEBUG message only after another test enabled debug logging.
	tc.Env.Log.SetLevel(logx.Debug)
	if code := a.Main(io.Discard, "--config-file", "/nonexistent.file", config.CommandName); code != 4 {
		t.Fatal(code, tc.Env.Log.Log())
	}
	if !strings.Contains(tc.Env.Log.Log(), "Configuration file does not exist") {
		t.Fatal(tc.Env.Log.Log())
	}
}

func TestApp_mainInvalidAccessTokens(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	a.Clients = append(a.Clients, CastorClient(nil, nil))
	empty := filepath.Join(t.TempDir(), "empty-access-tokens.json")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code := a.Main(io.Discard, "--config-file", tc.ConfigFile, "--access-token-file", empty, CastorClientName, GetTelemetryCommand, "1")
	if code != 5 || !strings.Contains(tc.Env.Log.Log(), "reading token store failed") {
		t.Fatal(code, tc.Env.Log.Log())
	}
}

// Beyond the Java suite: an uncaught runtime exception goes to stderr with status 1.
func TestApp_mainUncaughtRuntimeException(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	a, _ := app(t, tc, true)
	var stderr bytes.Buffer
	if code := a.Main(&stderr, "--config-file", tc.ConfigFile, AmphoraClientName, CreateSecretCommand); code != 1 {
		t.Fatal(code)
	}
	if !strings.HasPrefix(stderr.String(), `Exception in thread "main" java.lang.IllegalArgumentException: No secrets provided.`) {
		t.Fatal(stderr.String())
	}
}

// ---- ConfigurationTest (runs the application with a configure command that only loads the file) ----

func configApp(t *testing.T) (*App, *config.Env) {
	t.Helper()
	e, _ := testutil.Env(t, "", nil)
	e.Log = logx.New(io.Discard)
	e.Log.SetLevel(logx.Debug) // the debug messages the Java tests look for
	a := &App{Env: e, Configure: &config.Command{Run: func(e *config.Env) error { e.LoadFromFile(); return nil }},
		Login: login.NewCommand(), Clients: []Client{AmphoraClient(nil)}}
	return a, e
}

func random(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func TestConfiguration_givenCustomConfigDefinedAbsolute_whenStartCli_thenSetConfigPath(t *testing.T) {
	a, e := configApp(t)
	f := "/" + random(10)
	if err := a.Run("--config-file", f, "configure"); err != nil || e.ConfigPath != f {
		t.Fatal(err, e.ConfigPath)
	}
}

func TestConfiguration_givenCustomConfigDefinedRelative_whenStartCli_thenConvertToAbsoluteConfigPath(t *testing.T) {
	a, e := configApp(t)
	f := random(10)
	if err := a.Run("--config-file", f, "configure"); err != nil || e.ConfigPath != filepath.Join(e.WorkDir, f) {
		t.Fatal(err, e.ConfigPath)
	}
}

func TestConfiguration_givenConfigIsInvalid_whenStartCli_thenLogError(t *testing.T) {
	a, e := configApp(t)
	f := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(f, nil, 0o644)
	if err := a.Run("--config-file", f, "configure"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Log.Log(), props.Get(props.Configuration, "read-config.failure.cannot-be-parsed")) {
		t.Fatal(e.Log.Log())
	}
}

func TestConfiguration_givenConfigCannotBeRead_whenStartCli_thenLogError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any file")
	}
	a, e := configApp(t)
	f := filepath.Join(t.TempDir(), "config")
	_ = os.WriteFile(f, nil, 0o000)
	if err := a.Run("--config-file", f, "configure"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Log.Log(), props.Get(props.Configuration, "read-config.failure.cannot-be-read")) {
		t.Fatal(e.Log.Log())
	}
}

func TestConfiguration_givenConfigDoesNotExist_whenStartCli_thenLogError(t *testing.T) {
	a, e := configApp(t)
	if err := a.Run("--config-file", "/"+random(10), "configure"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Log.Log(), props.Get(props.Configuration, "read-config.log.does-not-exist")) {
		t.Fatal(e.Log.Log())
	}
}

func TestConfiguration_givenNoCustomConfigDefined_whenStartCli_thenReadFromDefaultPath(t *testing.T) {
	a, e := configApp(t)
	if err := a.Run("configure"); err != nil {
		t.Fatal(err)
	}
	log := e.Log.Log()
	if !strings.Contains(log, "Using config file") || !strings.Contains(log, filepath.Join(e.Home, config.FolderPath, config.FileName)) {
		t.Fatal(log)
	}
}
