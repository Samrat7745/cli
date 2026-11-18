// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// The files in testdata/java-golden are the stdout and exit status of the Java CLI 0.6.0
// (cli-0.6.0.jar) for the same command lines. The Go CLI must print the same text; the only
// differences allowed are the ones of a Go program: Java stack frames are not printed and exception
// lines carry no Java class name ("io.carbynestack...CsCliException: msg" is "msg" in Go).

var (
	javaFrame     = regexp.MustCompile(`^\s+at |^\s*\.\.\. \d+ more$`)
	javaClassName = regexp.MustCompile(`^(Caused by: )?(?:[a-zA-Z_$][\w$]*\.)+[A-Z][\w$]*(?:: )?`)
)

func normalizeJava(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if javaFrame.MatchString(l) {
			continue
		}
		out = append(out, javaClassName.ReplaceAllString(l, "$1"))
	}
	return strings.Join(out, "\n")
}

func TestGolden_javaCliOutput(t *testing.T) {
	const u = "3d908814-02a1-44f6-9e93-0d1d321eec3f"
	cases := map[string]string{
		// no configuration needed
		"help": "--help", "amphora-help": "amphora --help", "castor-help": "castor --help",
		"ephemeral-help": "ephemeral --help", "thymus-help": "thymus --help", "configure-help": "configure --help",
		"login-help": "login --help", "nocmd": "", "debug-nocmd": "--debug", "invalid-cmd": "invalidCommand",
		"config-only": "--config-file", "unknown-opt": "--bogus",
		// with a configuration (and no token file)
		"amphora-nosub":     "@ amphora",
		"getsecret-missing": "@ amphora get-secret",
		"getsecret-arity":   "@ amphora get-secret " + u + " " + u,
		"create-badtag":     "@ amphora create-secret -s 42 -t key1:value1",
		"upload-missing":    "@ castor upload-tuple -t MULTIPLICATION_TRIPLE_GFP 1",
		"eph-baduuid":       "@ ephemeral execute -i bad-uuid app",
		"thymus-badpolicy":  "@ thymus get-policy invalid_policy",
		"getsecrets-h":      "@ amphora get-secrets -h",
		"list-badsort":      "@ amphora get-secrets -s key:DOWN",
		"list-badfilter":    "@ amphora get-secrets -f key=",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "java-golden", name+".out"))
			if err != nil {
				t.Fatal(err)
			}
			code, _ := os.ReadFile(filepath.Join("testdata", "java-golden", name+".code"))
			wantCode, _ := strconv.Atoi(strings.TrimSpace(string(code)))

			e, out := testutil.Env(t, "", nil)
			args := strings.Fields(line)
			if len(args) > 0 && args[0] == "@" {
				cfg := testutil.Configuration(e, "https://127.0.0.1:1", "https://127.0.0.1:1")
				f := filepath.Join(t.TempDir(), "config")
				if err := os.WriteFile(f, []byte(cfg.PrettyString()), 0o644); err != nil {
					t.Fatal(err)
				}
				args = append([]string{"--config-file", f, "--access-token-file", filepath.Join(t.TempDir(), "none.json")}, args[1:]...)
			}
			a := &App{Env: e, Configure: &config.Command{}, Login: nil,
				Clients: []Client{AmphoraClient(nil), CastorClient(nil, nil), EphemeralClient(nil), ThymusClient(nil)}}
			a.Login = NewApp(e).Login
			gotCode := a.Main(io.Discard, args...)
			if gotCode != wantCode {
				t.Errorf("exit status %d, Java %d", gotCode, wantCode)
			}
			if got, w := out.String(), normalizeJava(string(want)); got != w {
				t.Errorf("output differs from the Java CLI\n--- Go\n%s\n--- Java (normalized)\n%s", got, w)
			}
		})
	}
}
