// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	ephemeral "github.com/Samrat7745/ephemeral/pkg/client"
	thymus "github.com/Samrat7745/thymus/services/pkg/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/props"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// ---- EphemeralClientCliTest ----

type fakeEphemeral struct {
	code    string
	inputs  []uuid.UUID
	results []ephemeral.Result
	err     error
}

func (f *fakeEphemeral) Execute(_ context.Context, code string, inputs []uuid.UUID) ([]ephemeral.Result, error) {
	f.code, f.inputs = code, inputs
	return f.results, f.err
}

func ephemeralCli(tc *testutil.TempConfig, mock *fakeEphemeral, args ...string) *ClientCli {
	var factory EphemeralFactory
	if mock != nil {
		factory = func(*EphemeralConfig) (EphemeralMulti, error) { return mock, nil }
	}
	return NewEphemeralCli(tc.Env, args, false, factory)
}

func ephemeralMsg(key string) string { return props.Get(props.Ephemeral, key) }

func TestEphemeral_givenHelpOption_whenParsing_thenShowUsage(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	c := ephemeralCli(tc, nil, "--help")
	if err := c.Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tc.Out.String(), "Usage: "+c.ProgramName()) {
		t.Fatal(tc.Out.String())
	}
}

func TestEphemeral_givenMissingCommand_whenParsing_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	if ce := asCli(t, ephemeralCli(tc, nil, "--debug").Parse()); !strings.HasPrefix(ce.Msg, props.Get(props.CsCli, "error.no-command")) {
		t.Fatal(ce.Msg)
	}
}

func TestEphemeral_givenMissingApplicationOption_whenParsing_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	ce := asCli(t, ephemeralCli(tc, nil, ExecuteCommand).Parse())
	if !strings.HasPrefix(ce.Msg, `Main parameters are required ("APPLICATION_NAME")`) {
		t.Fatal(ce.Msg)
	}
}

func TestEphemeral_givenInputOptionWithInvalidUUID_whenParsing_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	for _, invalid := range []string{
		"!" + uuid.NewString()[1:],
		// givenInputOptionWithWronglyConcatenatedUUIDs
		"3d908814-02a1-44f6-9e93-0d1d321eec3f:05c42ac1-7509-491e-93a4-11bc4bb28d64",
	} {
		tc := testutil.NewTempConfig(t, "", nil, nil)
		ce := asCli(t, ephemeralCli(tc, nil, ExecuteCommand, "-i", invalid, "app.example.com").Parse())
		if !strings.Contains(ce.Msg, props.Format(props.CsCli, "uuid.conversion.exception", invalid)) {
			t.Fatal(ce.Msg)
		}
	}
}

func TestEphemeral_givenUnreachableEphemeralUris_whenParsing_thenExceptionIsThrown(t *testing.T) {
	// The configuration points at 127.0.0.1:1, which refuses connections (apollo.example.com in Java).
	tc := testutil.NewTempConfig(t, "a = sint(1)", nil, func(e *config.Env) *config.Configuration {
		return testutil.Configuration(e, "https://127.0.0.1:1", "https://127.0.0.1:1")
	})
	re := asRunner(t, ephemeralCli(tc, nil, ExecuteCommand, "app.example.com").Parse())
	if !strings.HasPrefix(re.Msg, ephemeralMsg("execute.failure.invoke")) {
		t.Fatal(re.Msg)
	}
}

func inputsArg(ids []uuid.UUID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return strings.Join(s, ",")
}

func TestEphemeral_givenAllRequiredArguments_whenParsing_thenCallClientWithCorrectArguments(t *testing.T) {
	code := "a = sint(1)"
	tc := testutil.NewTempConfig(t, code, nil, nil)
	inputs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	out := uuid.NewString()
	mock := &fakeEphemeral{results: []ephemeral.Result{{Response: []string{out}}, {Response: []string{out}}}}
	if err := ephemeralCli(tc, mock, ExecuteCommand, "-i", inputsArg(inputs), "app.svc.example.com").Parse(); err != nil {
		t.Fatal(err)
	}
	if mock.code != code || !slices.Equal(mock.inputs, inputs) {
		t.Fatal(mock.code, mock.inputs)
	}
	if !strings.Contains(tc.Out.String(), "["+out+"]") {
		t.Fatal(tc.Out.String())
	}
}

func TestEphemeral_givenPlayersReturnDifferentResults_whenInvokingFunction_thenThrowException(t *testing.T) {
	tc := testutil.NewTempConfig(t, "a = sint(1)", nil, nil)
	o1, o2, o3 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mock := &fakeEphemeral{results: []ephemeral.Result{{Response: []string{o1, o2}}, {Response: []string{o1, o3}}}}
	err := ephemeralCli(tc, mock, ExecuteCommand, "-i", inputsArg([]uuid.UUID{uuid.New()}), "app.svc.example.com").Parse()
	if err == nil || err.Error() != ephemeralMsg("execute.failure.invoke-different-results") {
		t.Fatal(err)
	}
}

func TestEphemeral_givenClientReturnsWith500StatusCode_whenParsing_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "a = sint(1)", nil, nil)
	mock := &fakeEphemeral{err: &ephemeral.ActivationError{ResponseCode: 500, Message: "some error"}}
	re := asRunner(t, ephemeralCli(tc, mock, ExecuteCommand, "-i", inputsArg([]uuid.UUID{uuid.New()}), "app.svc.example.com").Parse())
	if want := props.Format(props.Ephemeral, "execute.failure.invoke-status-code", 500, "some error"); !strings.HasPrefix(re.Msg, want) {
		t.Fatalf("got %q want %q", re.Msg, want)
	}
}

func timeoutSeen(t *testing.T, args ...string) int {
	tc := testutil.NewTempConfig(t, "a = sint(1)", nil, nil)
	seen := -1
	factory := func(c *EphemeralConfig) (EphemeralMulti, error) {
		seen = c.Timeout
		return &fakeEphemeral{results: []ephemeral.Result{{Response: []string{uuid.NewString()}}}}, nil
	}
	if err := NewEphemeralCli(tc.Env, args, false, factory).Parse(); err != nil {
		t.Fatal(err)
	}
	return seen
}

func TestEphemeral_givenNoTimeoutOptionDefined_whenUsingEphemeralClient_thenUse10SecondsAsDefault(t *testing.T) {
	if got := timeoutSeen(t, ExecuteCommand, "DUMMY_APP_NAME"); got != 10 {
		t.Fatal(got)
	}
}

func TestEphemeral_givenTimeoutOptionSpecified_whenUsingEphemeralClient_thenUseGivenTimeout(t *testing.T) {
	timeout := rand.Intn(100) + 1
	if got := timeoutSeen(t, ExecuteCommand, "-t", strconv.Itoa(timeout), "DUMMY_APP_NAME"); got != timeout {
		t.Fatal(got, timeout)
	}
}

// ---- ThymusClientCliTest ----

type fakeThymus struct {
	policies []thymus.NamespacedName
	policy   *thymus.Policy
	err      error
	asked    []thymus.NamespacedName
}

func (f *fakeThymus) GetPolicies(context.Context) ([]thymus.NamespacedName, error) {
	return f.policies, f.err
}
func (f *fakeThymus) GetPolicy(_ context.Context, n thymus.NamespacedName) (*thymus.Policy, error) {
	f.asked = append(f.asked, n)
	return f.policy, f.err
}

func thymusCli(tc *testutil.TempConfig, mock *fakeThymus, args ...string) *ClientCli {
	var factory ThymusFactory
	if mock != nil {
		factory = func() (ThymusVC, error) { return mock, nil }
	}
	return NewThymusCli(tc.Env, args, false, factory)
}

var serviceError = &thymus.ServiceError{Endpoint: thymus.Endpoint{ServiceURI: "http://test-endpoint"}, ResponseCode: 500,
	Message: "Test error message"}

func TestThymus_givenHelpOption_whenParsing_thenShowUsage(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	c := thymusCli(tc, &fakeThymus{}, "--help")
	if err := c.Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tc.Out.String(), "Usage: "+c.ProgramName()) {
		t.Fatal(tc.Out.String())
	}
}

func TestThymus_givenMissingCommand_whenParsing_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	if ce := asCli(t, thymusCli(tc, &fakeThymus{}, "--debug").Parse()); !strings.HasPrefix(ce.Msg, props.Get(props.CsCli, "error.no-command")) {
		t.Fatal(ce.Msg)
	}
}

func TestThymus_givenMissingPolicyId_whenParsingGetPolicy_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	ce := asCli(t, thymusCli(tc, &fakeThymus{}, GetPolicyCommand).Parse())
	if want := `Main parameters are required ("` + props.Get(props.Thymus, "get-policy.parameter.policy-id-description") + `")`; !strings.HasPrefix(ce.Msg, want) {
		t.Fatal(ce.Msg)
	}
}

func TestThymus_givenCommunicationFails_whenGetPolicy_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	cause := &thymus.IOError{Err: errors.New("Test exception")}
	re := asRunner(t, thymusCli(tc, &fakeThymus{err: cause}, GetPolicyCommand, "test:policy").Parse())
	if want := props.Format(props.Thymus, "get-policy.failure", cause.Error()); !strings.HasPrefix(re.Msg, want) {
		t.Fatal(re.Msg)
	}
}

func TestThymus_givenInvalidPolicyName_whenGetPolicy_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	ce := asCli(t, thymusCli(tc, nil, GetPolicyCommand, "invalid_policy").Parse())
	if want := props.Format(props.Thymus, "policy-id.conversion.exception", "invalid_policy"); !strings.HasPrefix(ce.Msg, want) {
		t.Fatal(ce.Msg)
	}
}

func TestThymus_givenClientReturnsThymusServiceError_whenGetPolicy_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	re := asRunner(t, thymusCli(tc, &fakeThymus{err: serviceError}, GetPolicyCommand, "test:policy").Parse())
	if want := props.Format(props.Thymus, "get-policy.failure", serviceError.Error()); !strings.HasPrefix(re.Msg, want) {
		t.Fatal(re.Msg)
	}
}

func TestThymus_givenClientReturnsPolicy_whenGetPolicy_thenPrintPolicySource(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	name, _ := thymus.ParseNamespacedName("test:policy")
	mock := &fakeThymus{policy: &thymus.Policy{Name: name, Source: "test source"}}
	if err := thymusCli(tc, mock, GetPolicyCommand, "test:policy").Parse(); err != nil {
		t.Fatal(err)
	}
	if len(mock.asked) != 1 || mock.asked[0] != name || !strings.Contains(tc.Out.String(), "test source") {
		t.Fatal(mock.asked, tc.Out.String())
	}
}

func TestThymus_givenCommunicationFails_whenListPolicies_thenExceptionWithCorrectMessageIsThrown(t *testing.T) {
	for _, cause := range []error{&thymus.IOError{Err: errors.New("Test exception")}, serviceError} {
		tc := testutil.NewTempConfig(t, "", nil, nil)
		re := asRunner(t, thymusCli(tc, &fakeThymus{err: cause}, ListPoliciesCommand).Parse())
		if want := props.Format(props.Thymus, "list-policies.failure", cause.Error()); !strings.HasPrefix(re.Msg, want) {
			t.Fatal(re.Msg)
		}
	}
}

func TestThymus_givenClientReturnsPolicies_whenListPolicies_thenPrintPolicyNames(t *testing.T) {
	tc := testutil.NewTempConfig(t, "", nil, nil)
	p1, _ := thymus.ParseNamespacedName("test:policy1")
	p2, _ := thymus.ParseNamespacedName("test:policy2")
	if err := thymusCli(tc, &fakeThymus{policies: []thymus.NamespacedName{p1, p2}}, ListPoliciesCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if out := tc.Out.String(); !strings.Contains(out, "test:policy1") || !strings.Contains(out, "test:policy2") {
		t.Fatal(out)
	}
}
