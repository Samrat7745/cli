// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	castor "github.com/Samrat7745/castor/go/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// CastorClientName is the castor client.
const CastorClientName = "castor"

// Castor command names.
const (
	UploadTupleCommand   = "upload-tuple"
	GetTelemetryCommand  = "get-telemetry"
	ActivateChunkCommand = "activate-chunk"
)

// CastorCommunicationTimeout is the WebSocket connect timeout of upload-tuple.
const CastorCommunicationTimeout = 10 * time.Second

// CastorUpload is the part of the Castor upload client the CLI uses.
type CastorUpload interface {
	Connect(ctx context.Context, timeout time.Duration) error
	Disconnect() error
	UploadTupleChunk(ctx context.Context, chunk castor.TupleChunk, timeout time.Duration) (bool, error)
	ActivateTupleChunk(ctx context.Context, chunkID uuid.UUID) error
}

// CastorTelemetry is the part of the Castor intra-VCP client the CLI uses.
type CastorTelemetry interface {
	GetTelemetryData(ctx context.Context) (*castor.TelemetryData, error)
	GetTelemetryDataFor(ctx context.Context, interval int64) (*castor.TelemetryData, error)
}

// CastorUploadFactory and CastorTelemetryFactory create the clients; nil uses the configuration.
type (
	CastorUploadFactory    func() (CastorUpload, error)
	CastorTelemetryFactory func() (CastorTelemetry, error)
)

func ca(key string, args ...any) string { return props.Format(props.Castor, key, args...) }

// CastorClient registers the castor client.
func CastorClient(upload CastorUploadFactory, telemetry CastorTelemetryFactory) Client {
	return Client{Name: CastorClientName, Description: props.Get(props.Castor, "client-description"),
		New: func(e *config.Env, args []string, debug bool) *ClientCli {
			return NewCastorCli(e, args, debug, upload, telemetry)
		}}
}

// castorCommand holds the options every castor command has (CastorClientCliCommandConfig).
type castorCommand struct {
	help bool
	id   string
	set  bool
}

func (c *castorCommand) main() *jc.Main {
	return &jc.Main{Description: props.Get(props.Castor, "option.service-id-description"), Required: true, Arity: -1,
		Set: func(v string) error { c.id, c.set = v, true; return nil }}
}

// NewCastorCli is CastorClientCli: the castor commands.
func NewCastorCli(e *config.Env, args []string, debug bool, upload CastorUploadFactory, telemetry CastorTelemetryFactory) *ClientCli {
	c := &ClientCli{Env: e, Name: CastorClientName, Args: args, Debug: debug}
	runner := func(cc *castorCommand, run func(r *castorRunner) error) func() (Runner, error) {
		return func() (Runner, error) {
			r, err := newCastorRunner(e, cc.id, upload, telemetry)
			if err != nil {
				return nil, err
			}
			return runFunc(func() error { return run(r) }), nil
		}
	}

	{ // upload-tuple
		cc := &castorCommand{}
		var file, tupleType string
		var chunkID uuid.UUID
		c.Commands = append(c.Commands, &ClientCommand{Name: UploadTupleCommand,
			Description: props.Get(props.Castor, "upload.command-description"), Main: cc.main(),
			Options: []*jc.Option{
				hiddenHelp(&cc.help),
				{Names: []string{"-f", "--tuple-file"}, Description: props.Get(props.Castor, "upload.tuple-file-description"),
					Required: true, Order: -1, Kind: jc.Value, Arity: -1, Set: func(_, v string) error { file = v; return nil }},
				{Names: []string{"-t", "--tuple-type"}, Description: props.Get(props.Castor, "upload.tuple-type-description"),
					Required: true, Order: -1, Kind: jc.Value, Arity: -1, Set: func(_, v string) error { tupleType = v; return nil }},
				{Names: []string{"-i", "--chunk-id"}, Description: props.Get(props.Castor, "upload.option.chunk-id"),
					Order: 2, Kind: jc.Value, Arity: -1, Set: func(_, v string) error {
						s, err := parseUUIDArg(v)
						if err == nil {
							chunkID = uuid.MustParse(s)
						}
						return err
					}},
			},
			NewRunner: runner(cc, func(r *castorRunner) error {
				if chunkID == uuid.Nil {
					chunkID = uuid.New()
				}
				tt, err := castor.ParseTupleType(tupleType)
				if err != nil {
					return javautil.IllegalArgument("No enum constant io.carbynestack.castor.common.entities.TupleType."+tupleType, nil)
				}
				chunk, err := ParseTupleFile(r.env, tt, file, chunkID)
				if err != nil {
					return err
				}
				defer func() { _ = r.upload.Disconnect() }()
				_, err = Retry(r.env, 3, func() (any, error) {
					if err := r.upload.Connect(context.Background(), CastorCommunicationTimeout); err != nil {
						return nil, err
					}
					r.env.Log.Debugf(fmt.Sprintf("Uploading %d tuples to chunk #%s...", chunk.NumberOfTuples(), chunk.ChunkID))
					ok, err := r.upload.UploadTupleChunk(context.Background(), chunk, 0)
					if err != nil {
						return nil, err
					}
					if !ok {
						return nil, runnerErr("Failed uploading tuples.", nil)
					}
					r.env.Log.Debugf(props.Get(props.Castor, "upload.log.success"))
					r.env.Println(props.Get(props.Castor, "upload.log.success"))
					return nil, nil
				})
				return err
			})})
	}
	{ // get-telemetry
		cc := &castorCommand{}
		var interval *string
		c.Commands = append(c.Commands, &ClientCommand{Name: GetTelemetryCommand,
			Description: props.Get(props.Castor, "download.telemetry.command-description"), Main: cc.main(),
			Options: []*jc.Option{
				hiddenHelp(&cc.help),
				{Names: []string{"-i", "--interval"}, Description: props.Get(props.Castor, "download.telemetry.interval-description"),
					Order: -1, Kind: jc.Value, Arity: -1, Set: func(_, v string) error { interval = &v; return nil }},
			},
			NewRunner: runner(cc, func(r *castorRunner) error {
				var data *castor.TelemetryData
				var err error
				useInterval := false
				var n int64
				if interval != nil {
					if n, err = javautil.ParseLong(*interval); err != nil {
						return javautil.NumberFormat(err.Error())
					}
					useInterval = n > 0
				}
				if useInterval {
					data, err = r.telemetry.GetTelemetryDataFor(context.Background(), n)
				} else {
					data, err = r.telemetry.GetTelemetryData(context.Background())
				}
				if err != nil {
					r.env.Log.Error(props.Get(props.Castor, "download.telemetry.log.failure"), err)
					return runnerErr("Failed fetching telemetry data", err)
				}
				out := TelemetryDataToString(data)
				r.env.Log.Debugf(ca("download.telemetry.log.success", out))
				r.env.Println(out)
				return nil
			})})
	}
	{ // activate-chunk
		cc := &castorCommand{}
		var chunkID uuid.UUID
		c.Commands = append(c.Commands, &ClientCommand{Name: ActivateChunkCommand,
			Description: props.Get(props.Castor, "upload.command-description"), Main: cc.main(),
			Options: []*jc.Option{
				hiddenHelp(&cc.help),
				{Names: []string{"-i", "--chunk-id"}, Description: props.Get(props.Castor, "activate.option.chunk-id"),
					Required: true, Order: -1, Kind: jc.Value, Arity: -1, Set: func(_, v string) error {
						s, err := parseUUIDArg(v)
						if err == nil {
							chunkID = uuid.MustParse(s)
						}
						return err
					}},
			},
			NewRunner: runner(cc, func(r *castorRunner) error {
				r.env.Log.Debugf(fmt.Sprintf("Activating tuple chunk #%s...", chunkID))
				if err := r.upload.ActivateTupleChunk(context.Background(), chunkID); err != nil {
					r.env.Log.Errorf(props.Get(props.Castor, "activate.log.failure"))
					r.env.Println(props.Get(props.Castor, "activate.log.failure"))
					return runnerErr(err.Error(), err)
				}
				// The Java CLI logs the success at ERROR level.
				r.env.Log.Errorf(props.Get(props.Castor, "activate.log.success"))
				r.env.Println(props.Get(props.Castor, "activate.log.success"))
				return nil
			})})
	}
	return c
}

type castorRunner struct {
	env       *config.Env
	upload    CastorUpload
	telemetry CastorTelemetry
}

// newCastorRunner is the CastorClientCliCommandRunner constructor.
func newCastorRunner(e *config.Env, id string, upload CastorUploadFactory, telemetry CastorTelemetryFactory) (*castorRunner, error) {
	cfg, err := e.Instance()
	if err != nil {
		return nil, err
	}
	n, err := javautil.ParseInt(id)
	if err != nil {
		return nil, javautil.NumberFormat(err.Error())
	}
	vcp, err := cfg.Provider(n)
	if err != nil {
		return nil, err
	}
	tokens, err := tokensByBaseURL(e)
	if err != nil {
		return nil, err
	}
	var bearer castor.BearerTokenProvider
	if tokens != nil {
		t, ok := tokens[vcp.BaseURL]
		if !ok {
			return nil, runnerErr("No matching OAuth2 access token found", nil)
		}
		bearer = func(string) string { return t.IDToken }
	}
	r := &castorRunner{env: e}
	fail := func(err error) error { return runnerErr(props.Get(props.Castor, "client-instantiation-failed"), err) }
	if upload != nil {
		r.upload, err = upload()
	} else {
		r.upload, err = castor.NewUploadClient(castor.Options{ServiceAddresses: []string{vcp.CastorURL()},
			TrustedCertificates: cfg.TrustedCertificates, NoSSLValidation: cfg.IsNoSSLValidation(), BearerTokenProvider: bearer})
	}
	if err != nil {
		return nil, fail(err)
	}
	if telemetry != nil {
		r.telemetry, err = telemetry()
	} else {
		o := castor.Options{ServiceAddresses: []string{vcp.CastorURL()}, NoSSLValidation: cfg.IsNoSSLValidation(),
			BearerTokenProvider: bearer}
		if !cfg.IsNoSSLValidation() {
			o.TrustedCertificates = cfg.TrustedCertificates
		}
		r.telemetry, err = castor.NewIntraVcpClient(o)
	}
	if err != nil {
		return nil, fail(err)
	}
	return r, nil
}

// ParseTupleFile is TupleFileParser.parse: the file without its MP-SPDZ header (an 8 byte little
// endian length followed by that many header bytes).
func ParseTupleFile(e *config.Env, tt castor.TupleType, file string, chunkID uuid.UUID) (castor.TupleChunk, error) {
	abs, _ := filepath.Abs(file)
	if fi, err := os.Stat(file); err != nil || !fi.Mode().IsRegular() {
		e.Log.Errorf(fmt.Sprintf("%s is not a file.", abs))
		return castor.TupleChunk{}, javautil.IllegalArgument("", nil)
	}
	f, err := os.Open(file)
	if err != nil {
		e.Log.Errorf(fmt.Sprintf("Cannot read file %s", file))
		return castor.TupleChunk{}, &javautil.RuntimeError{Class: "java.lang.RuntimeException"}
	}
	defer f.Close()
	var hdr [8]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		e.Log.Errorf(fmt.Sprintf("Cannot read file %s", file))
		return castor.TupleChunk{}, &javautil.RuntimeError{Class: "java.lang.RuntimeException"}
	}
	n := int64(binary.LittleEndian.Uint64(hdr[:]))
	if skipped, err := io.CopyN(io.Discard, f, n); err != nil || skipped != n {
		e.Log.Errorf(fmt.Sprintf("Cannot read file %s", file))
		return castor.TupleChunk{}, &javautil.RuntimeError{Class: "java.lang.RuntimeException"}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		e.Log.Errorf(fmt.Sprintf("Cannot read file %s", file))
		return castor.TupleChunk{}, &javautil.RuntimeError{Class: "java.lang.RuntimeException"}
	}
	return castor.TupleChunk{ChunkID: chunkID, TupleType: tt, Tuples: data}, nil
}

// TelemetryDataToString is TelemetryPrinter.telemetryDataToString.
func TelemetryDataToString(d *castor.TelemetryData) string {
	var b strings.Builder
	for _, m := range d.Metrics {
		name := m.Type
		if tt, err := castor.ParseTupleType(m.Type); err == nil {
			name = tt.String()
		}
		fmt.Fprintf(&b, "%s\n\tavailable:\t%d\n\tconsumption/s:\t%d\n\n", name, m.Available, m.ConsumptionRate)
	}
	fmt.Fprintf(&b, "interval: %d", d.Interval)
	return b.String()
}

// Retry is util.Retry.retry: call f up to tries times; the last failure is wrapped in "Failed on retry.".
func Retry[T any](e *config.Env, tries int, f func() (T, error)) (T, error) {
	var zero T
	if tries <= 0 {
		return zero, javautil.IllegalArgument("Number of tries must be larger 0.", nil)
	}
	for i := 1; i <= tries; i++ {
		v, err := f()
		if err == nil {
			return v, nil
		}
		e.Log.Debugf("Failed on attempt #{}: {}", i, err.Error())
		if i == tries {
			return zero, runnerErr("Failed on retry.", err)
		}
	}
	return zero, runnerErr("Unexpected behaviour on method execution", nil)
}
