# Java test cases -> Go tests

All 135 test methods of `src/test/java` (17 test classes) are ported. Paths are relative to `go/`;
`cli/` is `internal/cli`, `config/` is `internal/config`, `login/` is `internal/login`.

Mockito mocks are replaced by fakes that record calls (`fakeAmphora`, `fakeUpload`, `fakeTelemetry`,
`fakeEphemeral`, `fakeThymus`, `stubServer`); `TemporaryConfiguration`, `ConfigurationUtil` and
`TokenUtils` are `internal/testutil`; `SystemOutRule`, `TextFromStandardInputStream`,
`EnvironmentVariables` and `LoggingRule` are the `config.Env` of a test (captured stdout, given
stdin, given environment variables, the recorded log events). Test resources are read from
`../src/test/resources`.

## Deviations

| Java | Go | Why |
|---|---|---|
| `certA.pem`, `certB.pem`, `keyStoreA.jks` | `testutil.NewCert` generates two certificates per test | The Java certificates expired in 2018 and have no subject alternative name. Java trusts a pinned certificate as trust anchor without checking it; Go verifies the leaf (validity, SAN). |
| WireMock | `httptest` servers | |
| Tests that reach `apollo.example.com` (unreachable services, failing token refresh) | services on `127.0.0.1:1` | No DNS or network needed; the result (connection failure) is the same. |
| `mainInvalidConfigurationTest`, `givenConfigDoesNotExist`, `givenNoCustomConfigDefined`, `givenConfigurationFilePathUndefined` check DEBUG messages | the Go tests enable DEBUG | In Java these pass only because an earlier test (`setDebugTest`) left the static root logger at DEBUG. |
| `deleteSecretsFailesUnexpected` (`RuntimeException`) | a plain Go `error` | Any error that is not an Amphora `ClientError` is the "Unexpected Reason" case. |
| `getMetadataList` has a `null` tag | an empty tag list | A Go tag slice cannot hold nil; the printed output is the same. |
| `TestVcpConfiguration_givenExistingVcpConfigurationAndNewBaseUrlProvided` | unchanged random host | Like the Java test, it fails when the random host starts with a digit (about one run in six): `java.net.URI` has no host for e.g. `http://3abc/`, and the Go port reproduces that rule. |
| `givenCustomCertsDefined_whenRequestingServerWithCACert` | skipped without network | It calls `maps.googleapis.com`. |

## Test classes

### CsCliApplicationTest (12) -> cli/app_test.go
| showHelpTest | `TestApp_showHelp` |
|---|---|
| setDebugTest | `TestApp_setDebug` |
| setConfigFile | `TestApp_setConfigFile` |
| invalidCommandTest | `TestApp_invalidCommand` |
| detailedConfigurationHelpTest | `TestApp_detailedConfigurationHelp` |
| printsConfigurationInUseTest | `TestApp_printsConfigurationInUse` |
| commandExecutionTest | `TestApp_commandExecution` |
| mainInvalidCommandTest | `TestApp_mainInvalidCommand` (exit 1) |
| mainUnexpectedErrorTest | `TestApp_mainUnexpectedError` (exit 2) |
| mainCommandExecutionErrorTest | `TestApp_mainCommandExecutionError` (exit 3) |
| mainInvalidConfigurationTest | `TestApp_mainInvalidConfiguration` (exit 4) |
| mainInvalidAccessTokensTest | `TestApp_mainInvalidAccessTokens` (exit 5) |

### client/amphora/AmphoraClientCliTest (44) -> cli/amphora_test.go
| showHelpTest, noCommandTest | `TestAmphora_showHelp`, `TestAmphora_noCommand` |
|---|---|
| uploadMissingSecretsTest, uploadInvalidTagFormatTest, uploadFailedTest | `TestAmphora_uploadMissingSecrets`, `..._uploadInvalidTagFormat`, `..._uploadFailed` |
| downloadMissingIdOptionTest, downloadFailedTest | `TestAmphora_downloadMissingIdOption`, `..._downloadFailed` |
| uploadSuccessRandomIdTest, uploadSuccessMultipleSecrets, uploadSuccessMultipleSecretsFromStdin, uploadSuccessMultipleSecretsFromStdinSkipsEmptyLines, uploadSuccessWithIdTest, uploadSuccessWithOneTagTest, uploadSuccessWithMultipleTagsTest | `TestAmphora_uploadSuccess...` (same names) |
| downloadSuccessTest, downloadFailInvalidNumberOfMainParameters, downloadSecretVerificationFails, downloadLargeSecretSuccessTest, downloadSuccessWithTagsTest | `TestAmphora_download...` (same names) |
| listSuccessTest, testListSecretsFailed, listWithIdsOnlySuccessTest | `TestAmphora_listSuccess`, `..._listSecretsFailed`, `..._listWithIdsOnlySuccess` |
| givenInvalidTagFilterString..., givenEmptyTagFilterString... | `TestAmphora_givenInvalidTagFilterString...` (both inputs) |
| givenValidTagFilters... | `TestAmphora_givenValidTagFilters...` |
| givenInvalidSortConfig..., givenEmptySortConfig... | `TestAmphora_givenInvalidSortConfig...` (both inputs) |
| givenValidSortConfig... | `TestAmphora_givenValidSortConfig...` |
| deleteSecretsSuccess, deleteSecretsSuccessCli, deleteSecretsFailSuccessMix, deleteSecretsFailesUnexpected | `TestAmphora_deleteSecrets...` |
| testCreateTag, testGetTag, testGetTags, testOverwriteTags, testUpdateTag, testDeleteTag | `TestAmphora_createTag`, `..._getTag`, `..._getTags`, `..._overwriteTags`, `..._updateTag`, `..._deleteTag` |
| testCreateTagFailed, testGetTagFailed, testGetTagsFailed, testOverwriteTagsFailed, testUpdateTagFailed, testDeleteTagFailed | `TestAmphora_tagCommandsFailed` (one case per command) |

### client/amphora/AmphoraClientCliIT (4) -> cli/amphora_it_test.go
insecureConnectionTest, disableSslValidationTest, trustGivenCertificateTest, wrongTrustedCertificateTest ->
`TestAmphoraIT_insecureConnection`, `..._disableSslValidation`, `..._trustGivenCertificate`, `..._wrongTrustedCertificate`
(two HTTPS stubs, real Amphora client, addresses from `CS_VCP_<n>_AMPHORA_URL`).

### client/castor/CastorClientCliTest (11) -> cli/castor_test.go
showHelpTest, noCommandTest, uploadTupleMissingTripleFileOptionTest, uploadTupleMissingCastorServiceIdTest,
uploadTupleMissingTupleTypeTest, uploadTuplesSuccessTest, getTelemetryConnectionRefusedTest,
getTelemetryWithIntervalSuccessTest, activateChunkMissingIdTest, activateChunkInvalidIdTest, activateChunkSuccessTest ->
`TestCastor_<same name without "Test">`.

### client/castor/util/TupleFileParserTest (2) -> cli/castor_test.go
`TestTupleFileParser_parsesInputsToTupleChunk` (also checks 10000 whole tuples), `TestTupleFileParser_handlesNonFileAsFileInput`.

### client/ephemeral/EphemeralClientCliTest (11) -> cli/ephemeral_thymus_test.go
All `TestEphemeral_<same name>`; the two invalid UUID tests (invalid character, two concatenated UUIDs) are
cases of `TestEphemeral_givenInputOptionWithInvalidUUID...`.

### client/thymus/ThymusClientCliTest (10) -> cli/ephemeral_thymus_test.go
All `TestThymus_<same name>`; communication failure and service error of list-policies are cases of
`TestThymus_givenCommunicationFails_whenListPolicies...`.

### configuration/ConfigurationCommandTest (4) -> config/config_test.go
`TestConfigurationCommand_<same name>`.

### configuration/ConfigurationTest (6) -> cli/app_test.go
`TestConfiguration_<same name>` (they run the application, which lives in package cli).

### configuration/VcpConfigurationTest (5) -> config/config_test.go
The three environment variable tests (Amphora, Castor, Ephemeral) are `TestVcpConfiguration_givenEnvVariableForVcp1Services...`;
`..._givenNewVcpConfigurationAndBaseUrlNotProvided...`, `..._givenExistingVcpConfigurationAndNewBaseUrlProvided...`.

### converter/PathTypeConverterTest (1) -> cli/converter_test.go
`TestPathTypeConverter_givenInvalidPath_whenConverting_thenThrows`.

### login/LoginCommandTest (5) -> login/login_test.go
`TestLoginCommand_<same name>`.

### login/OAuth2AuthenticationCodeCallbackHttpServerTest (4) -> login/login_test.go
`TestCallbackServer_<same name>` (on a free port instead of the fixed port 32900).

### login/RangeUtilTest (3) -> login/login_test.go
`TestRangeUtil` (the three cases).

### login/VcpTokenStoreTest (6) -> login/login_test.go
`TestVcpTokenStore_<same name>`.

### util/OAuthUtilTest (4) -> login/login_test.go
`TestOAuthUtil_<same name>` (the OAuth2 HTTP client is `login.HTTPClient`).

### util/RetryTest (3) -> cli/castor_test.go
`TestRetry_<same name>`.

## Tests without a Java counterpart

- `cli/golden_test.go`: the stdout and exit status of 22 command lines (help of every command, parse
  errors of every client, invalid sort and filter) are compared with the Java CLI 0.6.0 output in
  `internal/cli/testdata/java-golden`. Only Java stack frames and Java class names are removed from the
  Java side; everything else must be byte-identical.
- `TestLoginCommand_endToEndWithCallbackServerAndTokenEndpoint`: a full login with the real callback
  server and a stub token endpoint (authorization URL parameters, token request form, stored tokens).
- `TestCallbackServer_givenWrongState_thenRejects`, `TestVcpTokenStore_fileFormat`,
  `TestCastor_uploadTuplesRefusedIsRetried`, `TestCastor_telemetryPrinter`, `TestAmphora_tagConverterLongValues`,
  `TestApp_mainUncaughtRuntimeException`, `TestConfigurationCommand_interactiveConfigurationIsStored`,
  `TestConfiguration_prettyStringAndParseRoundTrip`.
- `internal/jc`, `internal/props`, `internal/javautil`: the JCommander rules, the `.properties` and
  `MessageFormat` handling (and that the embedded bundles equal `src/main/resources`), Java's lenient
  `UUID.fromString` and number parsing.

Run: `cd go && go test -race ./...`
