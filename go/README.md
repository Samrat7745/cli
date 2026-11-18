# Carbyne Stack CLI (Go)

Go implementation of the Carbyne Stack command line interface `cs`. It is a drop-in replacement for
the Java CLI in this repository (`cli-0.6.0`): same commands, options, messages, exit codes,
configuration file (`~/.cs/config`) and token file (`~/.cs/access-tokens.json`). The Java sources stay
in the repository as the reference implementation.

The service calls use the Go clients of the migrated services: `amphora/go/client`,
`castor/go/client`, `ephemeral/pkg/client` and `thymus/services/pkg/client`.

## Build and run

```bash
cd go
go build -o cs ./cmd/cs
./cs --help
./cs --config-file ~/.cs/config amphora get-secrets
```

A single static binary; no JVM. Start-up takes under 10 ms (`cs --help`); the Java CLI needs about
0.4 s for the JVM alone and about 2 s for a `create-secret` call against the test clusters.

## Commands

| Command | |
|---|---|
| `cs configure [show]` | interactive configuration, or print it |
| `cs login` | OAuth2 login to every provider (opens the browser, callback on port 32768 and up) |
| `cs amphora create-secret / get-secret / get-secrets / create-tag / get-tags / overwrite-tags / get-tag / update-tag / delete-tag / delete-secrets` | secrets |
| `cs castor upload-tuple / get-telemetry / activate-chunk <provider>` | tuples |
| `cs ephemeral execute [-i <secret-id>...] <application>` | run a program read from stdin |
| `cs thymus list-policies / get-policy <namespace:name>` | policies |

Global options: `--config-file`, `--access-token-file`, `--debug`, `--help`. The environment
variables of `cs configure --help` (`CS_PRIME`, `CS_VCP_<n>_AMPHORA_URL`, ...) override the
configuration as in the Java CLI.

Exit codes: 0 success, 1 invalid command line or uncaught error, 2 command could not be set up,
3 command failed, 4 configuration error, 5 login error.

## Layout

| Package | Java counterpart |
|---|---|
| `cmd/cs` | `CsCliApplication.main` |
| `internal/cli` | `CsCliApplication`, `CsClientCli`, the amphora/castor/ephemeral/thymus commands, `SecretPrinter`, `TelemetryPrinter`, `TupleFileParser`, `Retry`, the converters |
| `internal/config` | `configuration` (`Configuration`, `VcpConfiguration`, `ConfigurationCommand`), `ConsoleReader` |
| `internal/login` | `login` (`LoginCommand`, callback server, `VcpTokenStore`, `BrowserLauncher`), `OAuthUtil` |
| `internal/jc` | the JCommander 1.72 parsing and usage rules the CLI depends on |
| `internal/props` | the message bundles (embedded copies of `src/main/resources/*.properties`), `MessageFormat`, `StringEscapeUtils.unescapeJava` |
| `internal/logx` | the log4j console setup (`log4j.properties`) |
| `internal/javautil` | Java behaviour the arguments depend on (`UUID.fromString`, number parsing, uncaught exceptions) |

## Behaviour kept from the Java CLI

These are kept on purpose so that scripts and users see no difference; they may be worth changing
upstream:

- **Every command refreshes the tokens.** `VcpTokenStore.refresh` refreshes tokens that are valid for
  less than one hour, and Hydra issues one hour tokens, so every command uses the refresh token and
  writes the token file. Two `cs` processes started at the same time therefore use the same refresh
  token, and Hydra revokes the whole login (refresh token reuse detection); `cs login` is needed again.
  Run CLI commands sequentially.
- The bearer token sent to the services is the OIDC ID token (the stack's Istio authentication
  checks it), not the access token.
- `ephemeral execute -t/--timeout` is parsed but not used.
- `cs <client> <command> --help` runs the command (the command level `--help` is accepted but not
  evaluated); `cs <client> --help` prints the client's usage.
- The second endpoint prompt of `cs configure` shows the authorization endpoint as default.
- A failed login callback port bind moves the callback server to the next port, but the
  `redirect_uri` sent to the authorization server keeps the configured port.
- Log output goes to stdout ("%m%n"); an error with a cause is followed by "Caused by: ..." lines
  (without Java stack frames).

## Tests

See [TESTS.md](TESTS.md). `go test -race ./...` runs all ported Java tests, the comparison with the Java
CLI's output (`internal/cli/testdata/java-golden`) and the Go-specific tests.

`scripts/e2e.sh` tests the binary against a running two-party stack after `cs login`: configure,
castor telemetry, thymus policies, every amphora command, three Millionaires' games through
ephemeral and, with `JAVA_CLI_JAR`, secrets written by one CLI and read by the other. On the kind
deployment of the carbynestack repository (0.1.4-go services) all 24 checks pass, with the login
done by this CLI. `amphora overwrite-tags` fails there with HTTP 500 and exit 3, exactly like the
Java CLI: it is the Java Amphora service's `PUT /tags` bug, which the Go service keeps by default
(`COMPAT_JAVA_TAG_REPLACE_BUG`).
