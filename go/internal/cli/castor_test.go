// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	castor "github.com/Samrat7745/castor/go/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/props"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

const (
	castorURL          = "ws://localhost:8080/ws"
	testTripleFileName = "Triples-p-P0-10000"
)

type fakeUpload struct {
	uploaded  []castor.TupleChunk
	activated []uuid.UUID
	result    bool
	connects  int
}

func (f *fakeUpload) Connect(context.Context, time.Duration) error { f.connects++; return nil }
func (f *fakeUpload) Disconnect() error                            { return nil }
func (f *fakeUpload) UploadTupleChunk(_ context.Context, c castor.TupleChunk, _ time.Duration) (bool, error) {
	f.uploaded = append(f.uploaded, c)
	return f.result, nil
}
func (f *fakeUpload) ActivateTupleChunk(_ context.Context, id uuid.UUID) error {
	f.activated = append(f.activated, id)
	return nil
}

type fakeTelemetry struct {
	intervals []int64
	data      *castor.TelemetryData
	err       error
}

func (f *fakeTelemetry) GetTelemetryData(context.Context) (*castor.TelemetryData, error) {
	return f.data, f.err
}
func (f *fakeTelemetry) GetTelemetryDataFor(_ context.Context, i int64) (*castor.TelemetryData, error) {
	f.intervals = append(f.intervals, i)
	return f.data, f.err
}

type castorFixture struct {
	tc        *testutil.TempConfig
	upload    *fakeUpload
	telemetry *fakeTelemetry
}

func newCastorFixture(t *testing.T) *castorFixture {
	return &castorFixture{tc: testutil.NewTempConfig(t, "", nil, nil), upload: &fakeUpload{}, telemetry: &fakeTelemetry{}}
}

func (f *castorFixture) cli(args ...string) *ClientCli {
	return NewCastorCli(f.tc.Env, args, false,
		func() (CastorUpload, error) { return f.upload, nil },
		func() (CastorTelemetry, error) { return f.telemetry, nil })
}

func tupleFile() string { return testutil.Resources(testTripleFileName) }

func TestCastor_showHelp(t *testing.T) {
	f := newCastorFixture(t)
	c := f.cli("--help")
	if err := c.Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.tc.Out.String(), "Usage: "+c.ProgramName()) {
		t.Fatal(f.tc.Out.String())
	}
}

func TestCastor_noCommand(t *testing.T) {
	f := newCastorFixture(t)
	if ce := asCli(t, f.cli("--debug").Parse()); !strings.HasPrefix(ce.Msg, props.Get(props.CsCli, "error.no-command")) {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_uploadTupleMissingTripleFileOption(t *testing.T) {
	f := newCastorFixture(t)
	ce := asCli(t, f.cli(UploadTupleCommand, "-t", "MULTIPLICATION_TRIPLE_GFP", "1").Parse())
	if !strings.HasPrefix(ce.Msg, "The following option is required:") || !strings.Contains(ce.Msg, "--tuple-file") {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_uploadTupleMissingCastorServiceId(t *testing.T) {
	f := newCastorFixture(t)
	c := NewCastorCli(f.tc.Env, []string{UploadTupleCommand, "-t", "MULTIPLICATION_TRIPLE_GFP", "-f", tupleFile()}, false, nil, nil)
	if ce := asCli(t, c.Parse()); !strings.HasPrefix(ce.Msg, "Main parameters are required") {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_uploadTupleMissingTupleType(t *testing.T) {
	f := newCastorFixture(t)
	ce := asCli(t, f.cli(UploadTupleCommand, "-f", tupleFile()).Parse())
	if !strings.HasPrefix(ce.Msg, "The following option is required:") || !strings.Contains(ce.Msg, "--tuple-type") {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_uploadTuplesSuccess(t *testing.T) {
	f := newCastorFixture(t)
	f.upload.result = true
	id := uuid.New()
	err := f.cli(UploadTupleCommand, "-t", "MULTIPLICATION_TRIPLE_GFP", "-f", tupleFile(), "-i", id.String(), "1").Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.upload.uploaded) != 1 || f.upload.uploaded[0].ChunkID != id {
		t.Fatal(f.upload.uploaded)
	}
	if !strings.Contains(f.tc.Out.String(), props.Get(props.Castor, "upload.log.success")) {
		t.Fatal(f.tc.Out.String())
	}
}

// Beyond the Java suite: a refused upload is retried three times and then fails.
func TestCastor_uploadTuplesRefusedIsRetried(t *testing.T) {
	f := newCastorFixture(t)
	err := f.cli(UploadTupleCommand, "-t", "MULTIPLICATION_TRIPLE_GFP", "-f", tupleFile(), "1").Parse()
	re := asRunner(t, err)
	if re.Msg != "Failed on retry." || f.upload.connects != 3 || len(f.upload.uploaded) != 3 {
		t.Fatal(re.Msg, f.upload.connects, len(f.upload.uploaded))
	}
}

func TestCastor_getTelemetryConnectionRefused(t *testing.T) {
	f := newCastorFixture(t)
	cause := &castor.ClientError{Msg: "Connection refused"}
	f.telemetry.err = cause
	re := asRunner(t, f.cli(GetTelemetryCommand, "1").Parse())
	if re.Msg != "Failed fetching telemetry data" || re.Cause != cause {
		t.Fatal(re.Msg, re.Cause)
	}
}

func TestCastor_getTelemetryWithIntervalSuccess(t *testing.T) {
	f := newCastorFixture(t)
	f.telemetry.data = &castor.TelemetryData{Metrics: []castor.TupleMetric{}, Interval: 42000}
	if err := f.cli(GetTelemetryCommand, "-i", "42", "1").Parse(); err != nil {
		t.Fatal(err)
	}
	if len(f.telemetry.intervals) != 1 || f.telemetry.intervals[0] != 42 {
		t.Fatal(f.telemetry.intervals)
	}
	if !strings.Contains(f.tc.Out.String(), TelemetryDataToString(f.telemetry.data)) {
		t.Fatal(f.tc.Out.String())
	}
}

// Beyond the Java suite: the printed telemetry uses the Java TupleType.toString names.
func TestCastor_telemetryPrinter(t *testing.T) {
	got := TelemetryDataToString(&castor.TelemetryData{Interval: 60000, Metrics: []castor.TupleMetric{
		{Type: "BIT_GFP", Available: 100000, ConsumptionRate: 3}}})
	want := "bit_gfp\n\tavailable:\t100000\n\tconsumption/s:\t3\n\ninterval: 60000"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestCastor_activateChunkMissingId(t *testing.T) {
	f := newCastorFixture(t)
	ce := asCli(t, f.cli(ActivateChunkCommand, "1").Parse())
	if !strings.HasPrefix(ce.Msg, "The following option is required:") || !strings.Contains(ce.Msg, "--chunk-id") {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_activateChunkInvalidId(t *testing.T) {
	f := newCastorFixture(t)
	ce := asCli(t, f.cli(ActivateChunkCommand, castorURL, "-i", "invalidUUID").Parse())
	if !strings.HasPrefix(ce.Msg, "Invalid UUID") || !strings.Contains(ce.Msg, "invalidUUID") {
		t.Fatal(ce.Msg)
	}
}

func TestCastor_activateChunkSuccess(t *testing.T) {
	f := newCastorFixture(t)
	id := uuid.New()
	if err := f.cli(ActivateChunkCommand, "-i", id.String(), "1").Parse(); err != nil {
		t.Fatal(err)
	}
	if len(f.upload.activated) != 1 || f.upload.activated[0] != id {
		t.Fatal(f.upload.activated)
	}
}

// ---- TupleFileParserTest ----

var knownHeader = []byte{29, 0, 0, 0, 0, 0, 0, 0, 83, 80, 68, 90, 32, 103, 102, 112, 0, 16, 0, 0, 0, 0x80, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 27, 0x80, 1}

func TestTupleFileParser_parsesInputsToTupleChunk(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	tt, _ := castor.ParseTupleType("MULTIPLICATION_TRIPLE_GFP")
	id := uuid.New()
	chunk, err := ParseTupleFile(e, tt, tupleFile(), id)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.TupleType.Name() != "MULTIPLICATION_TRIPLE_GFP" || chunk.ChunkID != id {
		t.Fatal(chunk.TupleType, chunk.ChunkID)
	}
	if bytes.HasPrefix(chunk.Tuples, knownHeader) {
		t.Fatal("tuples still start with the MP-SPDZ header")
	}
	if chunk.NumberOfTuples() != 10000 || len(chunk.Tuples)%tt.TupleSize() != 0 {
		t.Fatal(chunk.NumberOfTuples(), len(chunk.Tuples))
	}
}

func TestTupleFileParser_handlesNonFileAsFileInput(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	tt, _ := castor.ParseTupleType("MULTIPLICATION_TRIPLE_GFP")
	_, err := ParseTupleFile(e, tt, t.TempDir(), uuid.New())
	var rt *javautil.RuntimeError
	if !errors.As(err, &rt) || rt.Class != "java.lang.IllegalArgumentException" {
		t.Fatal(err)
	}
}

// ---- RetryTest ----

func TestRetry_givenNonPositiveNumberOfTries_whenRetrying_thenThrows(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	_, err := Retry(e, -1, func() (any, error) { return struct{}{}, nil })
	var rt *javautil.RuntimeError
	if !errors.As(err, &rt) || rt.Class != "java.lang.IllegalArgumentException" {
		t.Fatal(err)
	}
}

func TestRetry_givenCalleeFailsNTimes_whenRetryingNMinus1Times_thenThrowAfterNMinus1Tries(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	tried := 0
	_, err := Retry(e, 5, func() (any, error) {
		tried++
		if tried-1 < 5 {
			return nil, errors.New("fail")
		}
		return nil, nil
	})
	if err == nil || tried != 5 {
		t.Fatal(err, tried)
	}
}

func TestRetry_givenCalleeFailsNMinus1Times_whenRetryingNTimes_thenReturnAfterNTries(t *testing.T) {
	e, _ := testutil.Env(t, "", nil)
	tried := 0
	want := &struct{ x int }{}
	got, err := Retry(e, 5, func() (any, error) {
		tried++
		if tried-1 < 4 {
			return nil, errors.New("fail")
		}
		return want, nil
	})
	if err != nil || got != want || tried != 5 {
		t.Fatal(err, tried)
	}
}
