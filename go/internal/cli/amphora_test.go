// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	amphora "github.com/Samrat7745/amphora/go/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/props"
	"github.com/Samrat7745/cli/go/internal/testutil"
)

// fakeAmphora is the Mockito mock of AmphoraClient: every call is recorded; the funcs give results.
type fakeAmphora struct {
	mu            sync.Mutex
	created       []amphora.Secret
	deleted       []uuid.UUID
	createTag     []any
	overwriteTags []any
	updateTag     []any
	deleteTag     []any
	getSecrets    [][]any

	createSecret func(amphora.Secret) (uuid.UUID, error)
	getSecret    func(uuid.UUID) (*amphora.Secret, error)
	deleteSecret func(uuid.UUID) error
	list         func([]amphora.TagFilter, *amphora.Sort) ([]amphora.Metadata, error)
	tagErr       error
	getTags      func(uuid.UUID) ([]amphora.Tag, error)
	getTag       func(uuid.UUID, string) (*amphora.Tag, error)
}

func (f *fakeAmphora) CreateSecret(_ context.Context, s amphora.Secret) (uuid.UUID, error) {
	f.mu.Lock()
	f.created = append(f.created, s)
	f.mu.Unlock()
	if f.createSecret != nil {
		return f.createSecret(s)
	}
	return uuid.Nil, nil
}
func (f *fakeAmphora) GetSecret(_ context.Context, id uuid.UUID) (*amphora.Secret, error) {
	if f.getSecret != nil {
		return f.getSecret(id)
	}
	return nil, nil
}
func (f *fakeAmphora) DeleteSecret(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	f.deleted = append(f.deleted, id)
	f.mu.Unlock()
	if f.deleteSecret != nil {
		return f.deleteSecret(id)
	}
	return nil
}
func (f *fakeAmphora) GetSecrets(_ context.Context, fs []amphora.TagFilter, s *amphora.Sort) ([]amphora.Metadata, error) {
	f.getSecrets = append(f.getSecrets, []any{fs, s})
	if f.list != nil {
		return f.list(fs, s)
	}
	return nil, nil
}
func (f *fakeAmphora) GetSecretsPage(context.Context, []amphora.TagFilter, amphora.PageRequest) (*amphora.MetadataPage, error) {
	return nil, nil
}
func (f *fakeAmphora) GetTags(_ context.Context, id uuid.UUID) ([]amphora.Tag, error) {
	return f.getTags(id)
}
func (f *fakeAmphora) CreateTag(_ context.Context, id uuid.UUID, t amphora.Tag) error {
	f.createTag = append(f.createTag, id, t)
	return f.tagErr
}
func (f *fakeAmphora) OverwriteTags(_ context.Context, id uuid.UUID, ts []amphora.Tag) error {
	f.overwriteTags = append(f.overwriteTags, id, ts)
	return f.tagErr
}
func (f *fakeAmphora) GetTag(_ context.Context, id uuid.UUID, k string) (*amphora.Tag, error) {
	return f.getTag(id, k)
}
func (f *fakeAmphora) UpdateTag(_ context.Context, id uuid.UUID, t amphora.Tag) error {
	f.updateTag = append(f.updateTag, id, t)
	return f.tagErr
}
func (f *fakeAmphora) DeleteTag(_ context.Context, id uuid.UUID, k string) error {
	f.deleteTag = append(f.deleteTag, id, k)
	return f.tagErr
}

type amphoraFixture struct {
	tc   *testutil.TempConfig
	mock *fakeAmphora
}

func newAmphoraFixture(t *testing.T, stdin string) *amphoraFixture {
	return &amphoraFixture{tc: testutil.NewTempConfig(t, stdin, nil, nil), mock: &fakeAmphora{}}
}

// cli is getCliWithArgs: an AmphoraClientCli with the mocked client.
func (f *amphoraFixture) cli(args ...string) *ClientCli {
	return NewAmphoraCli(f.tc.Env, args, false, func() (amphora.Client, error) { return f.mock, nil })
}

func (f *amphoraFixture) out() string { return f.tc.Out.String() }

func stringTag(k, v string) amphora.Tag {
	return amphora.Tag{Key: k, Value: v, ValueType: amphora.TagString}
}

func clientErr(msg string) error { return &amphora.ClientError{Msg: msg} }

func amphoraMsg(key string) string { return props.Get(props.Amphora, key) }

func asCli(t *testing.T, err error) *CliError {
	t.Helper()
	ce, ok := err.(*CliError)
	if !ok {
		t.Fatalf("expected CsCliException, got %T: %v", err, err)
	}
	return ce
}

func asRunner(t *testing.T, err error) *RunnerError {
	t.Helper()
	re, ok := err.(*RunnerError)
	if !ok {
		t.Fatalf("expected CsCliRunnerException, got %T: %v", err, err)
	}
	return re
}

func TestAmphora_showHelp(t *testing.T) {
	f := newAmphoraFixture(t, "")
	c := f.cli("--help")
	if err := c.Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out(), "Usage: "+c.ProgramName()) {
		t.Fatalf("usage missing:\n%s", f.out())
	}
}

func TestAmphora_noCommand(t *testing.T) {
	f := newAmphoraFixture(t, "")
	ce := asCli(t, f.cli("--debug").Parse())
	if !strings.HasPrefix(ce.Msg, props.Get(props.CsCli, "error.no-command")) {
		t.Fatal(ce.Msg)
	}
}

func TestAmphora_uploadMissingSecrets(t *testing.T) {
	f := newAmphoraFixture(t, "")
	err := f.cli(CreateSecretCommand).Parse()
	var rt *javautil.RuntimeError
	if !errors.As(err, &rt) || rt.Class != "java.lang.IllegalArgumentException" || rt.Msg != amphoraMsg("create.failure.read-secrets") {
		t.Fatalf("expected IllegalArgumentException(%q), got %v", amphoraMsg("create.failure.read-secrets"), err)
	}
}

func TestAmphora_uploadInvalidTagFormat(t *testing.T) {
	f := newAmphoraFixture(t, "")
	ce := asCli(t, f.cli(CreateSecretCommand, "-s", "42", "-t", "key1:value1").Parse())
	if !strings.Contains(ce.Msg, amphoraMsg("tag.conversion.exception.format")) {
		t.Fatal(ce.Msg)
	}
}

func TestAmphora_uploadFailed(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.createSecret = func(amphora.Secret) (uuid.UUID, error) { return uuid.Nil, clientErr("") }
	asRunner(t, f.cli(CreateSecretCommand, "42").Parse())
}

func TestAmphora_downloadMissingIdOption(t *testing.T) {
	f := newAmphoraFixture(t, "")
	ce := asCli(t, f.cli(GetSecretCommand).Parse())
	if !strings.HasPrefix(ce.Msg, `Main parameters are required ("SECRET_ID")`) {
		t.Fatal(ce.Msg)
	}
}

func TestAmphora_downloadFailed(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.getSecret = func(uuid.UUID) (*amphora.Secret, error) { return nil, clientErr("") }
	asRunner(t, f.cli(GetSecretCommand, uuid.NewString()).Parse())
}

func (f *amphoraFixture) onlyCreated(t *testing.T) amphora.Secret {
	t.Helper()
	if len(f.mock.created) != 1 {
		t.Fatalf("createSecret called %d times", len(f.mock.created))
	}
	s := f.mock.created[0]
	if s.SecretID == uuid.Nil {
		t.Fatal("secret id not set")
	}
	return s
}

func bigs(vs ...string) []*big.Int {
	out := make([]*big.Int, len(vs))
	for i, v := range vs {
		out[i], _ = new(big.Int).SetString(v, 10)
	}
	return out
}

func equalBigs(a, b []*big.Int) bool {
	return slices.EqualFunc(a, b, func(x, y *big.Int) bool { return x.Cmp(y) == 0 })
}

func TestAmphora_uploadSuccessRandomId(t *testing.T) {
	f := newAmphoraFixture(t, "")
	if err := f.cli(CreateSecretCommand, "42").Parse(); err != nil {
		t.Fatal(err)
	}
	if s := f.onlyCreated(t); !equalBigs(s.Data, bigs("42")) {
		t.Fatal(s.Data)
	}
}

func TestAmphora_uploadSuccessMultipleSecrets(t *testing.T) {
	f := newAmphoraFixture(t, "")
	if err := f.cli(CreateSecretCommand, "42", "24").Parse(); err != nil {
		t.Fatal(err)
	}
	if s := f.onlyCreated(t); !equalBigs(s.Data, bigs("42", "24")) {
		t.Fatal(s.Data)
	}
}

func TestAmphora_uploadSuccessMultipleSecretsFromStdin(t *testing.T) {
	f := newAmphoraFixture(t, "123\n456\n789\n951\n")
	if err := f.cli(CreateSecretCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if s := f.onlyCreated(t); !equalBigs(s.Data, bigs("123", "456", "789", "951")) {
		t.Fatal(s.Data)
	}
}

func TestAmphora_uploadSuccessMultipleSecretsFromStdinSkipsEmptyLines(t *testing.T) {
	f := newAmphoraFixture(t, "123\n\n456\n\n789\n951\n")
	if err := f.cli(CreateSecretCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if s := f.onlyCreated(t); !equalBigs(s.Data, bigs("123", "456", "789", "951")) {
		t.Fatal(s.Data)
	}
}

func TestAmphora_uploadSuccessWithId(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	f.mock.createSecret = func(amphora.Secret) (uuid.UUID, error) { return id, nil }
	if err := f.cli(CreateSecretCommand, "42", "-i", id.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	s := f.onlyCreated(t)
	if s.SecretID != id || !equalBigs(s.Data, bigs("42")) {
		t.Fatal(s)
	}
	if !strings.Contains(f.out(), id.String()) {
		t.Fatal("secret id not printed")
	}
}

func TestAmphora_uploadSuccessWithOneTag(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.createSecret = func(amphora.Secret) (uuid.UUID, error) { return uuid.New(), nil }
	if err := f.cli(CreateSecretCommand, "42", "-t", "key_1=value_1").Parse(); err != nil {
		t.Fatal(err)
	}
	s := f.onlyCreated(t)
	if !equalBigs(s.Data, bigs("42")) || !slices.Equal(s.Tags, []amphora.Tag{stringTag("key_1", "value_1")}) {
		t.Fatal(s)
	}
}

func TestAmphora_uploadSuccessWithMultipleTags(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.createSecret = func(amphora.Secret) (uuid.UUID, error) { return uuid.New(), nil }
	tag3 := stringTag("key.3", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789äöüÄÖÜ-_.:,;#'*+~!\"§$%&/()=?\\ß`'^°@€<>|")
	err := f.cli(CreateSecretCommand, "42", "-t", "key_1=value_1", "-t", "2-key=2-value",
		"-t", tag3.Key+"="+tag3.Value).Parse()
	if err != nil {
		t.Fatal(err)
	}
	s := f.onlyCreated(t)
	want := []amphora.Tag{stringTag("key_1", "value_1"), stringTag("2-key", "2-value"), tag3}
	for _, w := range want {
		if !slices.Contains(s.Tags, w) {
			t.Fatalf("tag %v missing in %v", w, s.Tags)
		}
	}
	if len(s.Tags) != 3 {
		t.Fatal(s.Tags)
	}
}

func TestAmphora_downloadSuccess(t *testing.T) {
	f := newAmphoraFixture(t, "")
	expected := &amphora.Secret{SecretID: uuid.New(), Tags: []amphora.Tag{}, Data: bigs("10")}
	f.mock.getSecret = func(id uuid.UUID) (*amphora.Secret, error) {
		if id != expected.SecretID {
			t.Fatal("wrong id")
		}
		return expected, nil
	}
	if err := f.cli(GetSecretCommand, expected.SecretID.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(f.out()); got != "[10]" {
		t.Fatalf("got %q", got)
	}
}

func TestAmphora_downloadFailInvalidNumberOfMainParameters(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.NewString()
	ce := asCli(t, f.cli(GetSecretCommand, id, id).Parse())
	if !strings.Contains(ce.Msg, "There should be exactly 1 main parameters but 2 were found") {
		t.Fatal(ce.Msg)
	}
}

func TestAmphora_downloadSecretVerificationFails(t *testing.T) {
	f := newAmphoraFixture(t, "")
	cause := &amphora.IntegrityError{Msg: "Verification failed."}
	f.mock.getSecret = func(uuid.UUID) (*amphora.Secret, error) { return nil, cause }
	re := asRunner(t, f.cli(GetSecretCommand, uuid.NewString()).Parse())
	if re.Msg != "Failed fetching secret." || re.Cause != cause {
		t.Fatal(re.Msg, re.Cause)
	}
}

func TestAmphora_downloadLargeSecretSuccess(t *testing.T) {
	f := newAmphoraFixture(t, "")
	expected := &amphora.Secret{SecretID: uuid.New(), Tags: []amphora.Tag{}, Data: bigs("10", "1", "0")}
	f.mock.getSecret = func(uuid.UUID) (*amphora.Secret, error) { return expected, nil }
	if err := f.cli(GetSecretCommand, expected.SecretID.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(f.out()); got != "[10, 1, 0]" {
		t.Fatalf("got %q", got)
	}
}

func TestAmphora_downloadSuccessWithTags(t *testing.T) {
	f := newAmphoraFixture(t, "")
	expected := &amphora.Secret{SecretID: uuid.New(), Tags: []amphora.Tag{stringTag("key_1", "value_1")}, Data: bigs("10")}
	f.mock.getSecret = func(uuid.UUID) (*amphora.Secret, error) { return expected, nil }
	if err := f.cli(GetSecretCommand, expected.SecretID.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(f.out()), SecretToString(expected); got != want || want != "[10]\n\tkey_1 -> value_1" {
		t.Fatalf("got %q want %q", got, want)
	}
}

// metadataList is AmphoraTestData.getMetadataList (the Java list's third entry has a null tag, which a
// Go tag slice cannot hold; it has no tags here).
func metadataList() []amphora.Metadata {
	tags := []amphora.Tag{{Key: "key1", Value: "tag1"}, {Key: "key2", Value: "tag2"}}
	return []amphora.Metadata{{SecretID: uuid.NewString(), Tags: tags}, {SecretID: uuid.NewString(), Tags: tags},
		{SecretID: uuid.NewString(), Tags: []amphora.Tag{}}}
}

func TestAmphora_listSuccess(t *testing.T) {
	f := newAmphoraFixture(t, "")
	list := metadataList()
	f.mock.list = func([]amphora.TagFilter, *amphora.Sort) ([]amphora.Metadata, error) { return list, nil }
	if err := f.cli(ListSecretsCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out(), MetadataListToString(list, false)) {
		t.Fatal(f.out())
	}
	if args := f.mock.getSecrets[0]; len(args[0].([]amphora.TagFilter)) != 0 || args[1].(*amphora.Sort) != nil {
		t.Fatal("expected getSecrets(emptyList(), null)")
	}
	want := list[0].SecretID + "\n\tkey1 -> tag1\n\tkey2 -> tag2\n" + list[1].SecretID + "\n\tkey1 -> tag1\n\tkey2 -> tag2\n" +
		list[2].SecretID + "\n"
	if MetadataListToString(list, false) != want {
		t.Fatalf("%q", MetadataListToString(list, false))
	}
}

func TestAmphora_listSecretsFailed(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.list = func([]amphora.TagFilter, *amphora.Sort) ([]amphora.Metadata, error) {
		return nil, clientErr("Sth went wrong")
	}
	if re := asRunner(t, f.cli(ListSecretsCommand).Parse()); !strings.Contains(re.Msg, "Sth went wrong") {
		t.Fatal(re.Msg)
	}
}

func TestAmphora_listWithIdsOnlySuccess(t *testing.T) {
	f := newAmphoraFixture(t, "")
	list := metadataList()
	f.mock.list = func([]amphora.TagFilter, *amphora.Sort) ([]amphora.Metadata, error) { return list, nil }
	if err := f.cli(ListSecretsCommand, "-l").Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out(), MetadataListToString(list, true)) {
		t.Fatal(f.out())
	}
	if MetadataListToString(list, true) != list[0].SecretID+"\n"+list[1].SecretID+"\n"+list[2].SecretID {
		t.Fatal("ids only output")
	}
}

func TestAmphora_givenInvalidTagFilterString_whenListSecrets_thenExceptionIsThrown(t *testing.T) {
	for _, filter := range []string{"key=", ""} { // + givenEmptyTagFilterString
		f := newAmphoraFixture(t, "")
		re := asRunner(t, f.cli(ListSecretsCommand, "-l", "-f", filter).Parse())
		if want := props.Format(props.Amphora, "list.failure.invalid-tag-filter", filter); re.Msg != want {
			t.Fatalf("%q: got %q want %q", filter, re.Msg, want)
		}
	}
}

func TestAmphora_givenValidTagFilters_whenListSecrets_thenCallClientWithExpectedArguments(t *testing.T) {
	f := newAmphoraFixture(t, "")
	if err := f.cli(ListSecretsCommand, "-l", "-f", "time<42", "-f", "type:temperature").Parse(); err != nil {
		t.Fatal(err)
	}
	want := []amphora.TagFilter{{Key: "time", Operator: "<", Value: "42"}, {Key: "type", Operator: ":", Value: "temperature"}}
	if got := f.mock.getSecrets[0][0].([]amphora.TagFilter); !slices.Equal(got, want) || f.mock.getSecrets[0][1].(*amphora.Sort) != nil {
		t.Fatal(got)
	}
}

func TestAmphora_givenInvalidSortConfig_whenListSecrets_thenExceptionIsThrown(t *testing.T) {
	for _, sort := range []string{"key:DOWN", ""} { // + givenEmptySortConfig
		f := newAmphoraFixture(t, "")
		re := asRunner(t, f.cli(ListSecretsCommand, "-l", "-s", sort).Parse())
		if want := props.Format(props.Amphora, "list.failure.invalid-sort-format", sort); re.Msg != want {
			t.Fatalf("got %q want %q", re.Msg, want)
		}
	}
}

func TestAmphora_givenValidSortConfig_whenListSecrets_thenCallClientWithExpectedArguments(t *testing.T) {
	f := newAmphoraFixture(t, "")
	if err := f.cli(ListSecretsCommand, "-l", "-s", "time:ASC").Parse(); err != nil {
		t.Fatal(err)
	}
	if s := f.mock.getSecrets[0][1].(*amphora.Sort); s == nil || *s != (amphora.Sort{Property: "time"}) {
		t.Fatal(s)
	}
}

func TestAmphora_deleteSecretsSuccess(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id1, id2 := uuid.New(), uuid.New()
	if err := f.cli(DeleteSecretsCommand, id1.String(), id2.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.deleted) != 2 || !slices.Contains(f.mock.deleted, id1) || !slices.Contains(f.mock.deleted, id2) {
		t.Fatal(f.mock.deleted)
	}
	if !strings.Contains(f.out(), "Secrets deleted") {
		t.Fatal(f.out())
	}
}

func TestAmphora_deleteSecretsSuccessCli(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	f := newAmphoraFixture(t, id1.String()+"\n"+id2.String()+"\n")
	if err := f.cli(DeleteSecretsCommand).Parse(); err != nil {
		t.Fatal(err)
	}
	if len(f.mock.deleted) != 2 || !slices.Contains(f.mock.deleted, id1) || !slices.Contains(f.mock.deleted, id2) {
		t.Fatal(f.mock.deleted)
	}
	if !strings.Contains(f.out(), "Secrets deleted") {
		t.Fatal(f.out())
	}
}

func TestAmphora_deleteSecretsFailSuccessMix(t *testing.T) {
	f := newAmphoraFixture(t, "")
	ok := map[uuid.UUID]bool{}
	args := []string{DeleteSecretsCommand}
	for i := 0; i < 100; i++ {
		id := uuid.New()
		ok[id] = i%3 != 0
		args = append(args, id.String())
	}
	f.mock.deleteSecret = func(id uuid.UUID) error {
		if !ok[id] {
			return clientErr("Unable to delete secret")
		}
		return nil
	}
	err := f.cli(args...).Parse()
	asRunner(t, err)
	line := regexp.MustCompile(`^(\S+).*([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}).*$`)
	seen := map[string]bool{}
	for _, l := range strings.Split(f.out(), "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		want := "Fail"
		if ok[uuid.MustParse(m[2])] {
			want = "Success"
		}
		if !strings.Contains(m[1], want) {
			t.Fatalf("line %q should say %s", l, want)
		}
		seen[m[2]] = true
	}
	if len(seen) != len(ok) {
		t.Fatalf("Number of received IDs not equal: %d != %d", len(seen), len(ok))
	}
}

func TestAmphora_deleteSecretsFailsUnexpected(t *testing.T) {
	f := newAmphoraFixture(t, "")
	f.mock.deleteSecret = func(uuid.UUID) error { return errors.New("Sth went wrong") }
	re := asRunner(t, f.cli(DeleteSecretsCommand, uuid.NewString()).Parse())
	if !strings.Contains(re.Msg, "Failed to delete secrets") {
		t.Fatal(re.Msg)
	}
	if !strings.Contains(f.out(), "Unexpected Reason: Unexpected exception Sth went wrong") {
		t.Fatal(f.out())
	}
}

func TestAmphora_createTag(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	if err := f.cli(CreateTagCommand, "-i", id.String(), "key=value").Parse(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.mock.createTag) != fmt.Sprint([]any{id, stringTag("key", "value")}) {
		t.Fatal(f.mock.createTag)
	}
}

func TestAmphora_tagCommandsFailed(t *testing.T) {
	id := uuid.NewString()
	for _, args := range [][]string{
		{CreateTagCommand, "-i", id, "key=value"},
		{OverwriteTagsCommand, "-i", id, "key=value"},
		{UpdateTagCommand, "-i", id, "key=value"},
		{DeleteTagCommand, "-i", id, "key"},
		{GetTagCommand, "-i", id, "key_1"},
		{GetTagsCommand, id},
	} {
		f := newAmphoraFixture(t, "")
		f.mock.tagErr = clientErr("Sth went wrong")
		f.mock.getTag = func(uuid.UUID, string) (*amphora.Tag, error) { return nil, f.mock.tagErr }
		f.mock.getTags = func(uuid.UUID) ([]amphora.Tag, error) { return nil, f.mock.tagErr }
		if re := asRunner(t, f.cli(args...).Parse()); !strings.Contains(re.Msg, "Sth went wrong") {
			t.Fatal(args[0], re.Msg)
		}
	}
}

func TestAmphora_getTag(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	tag := stringTag("key_1", "value_1")
	f.mock.getTag = func(i uuid.UUID, k string) (*amphora.Tag, error) {
		if i != id || k != tag.Key {
			t.Fatal("wrong arguments")
		}
		return &tag, nil
	}
	if err := f.cli(GetTagCommand, "-i", id.String(), tag.Key).Parse(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out(), strings.TrimSpace(TagToString(&tag))) {
		t.Fatal(f.out())
	}
}

func TestAmphora_getTags(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	tags := []amphora.Tag{stringTag("key_1", "value_1"), stringTag("2-key", "2-value")}
	f.mock.getTags = func(uuid.UUID) ([]amphora.Tag, error) { return tags, nil }
	if err := f.cli(GetTagsCommand, id.String()).Parse(); err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if !strings.Contains(f.out(), strings.TrimSpace(TagToString(&tag))) {
			t.Fatal(f.out())
		}
	}
}

func TestAmphora_overwriteTags(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	if err := f.cli(OverwriteTagsCommand, "-i", id.String(), "key=value").Parse(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.mock.overwriteTags) != fmt.Sprint([]any{id, []amphora.Tag{stringTag("key", "value")}}) {
		t.Fatal(f.mock.overwriteTags)
	}
}

func TestAmphora_updateTag(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	if err := f.cli(UpdateTagCommand, "-i", id.String(), "key=value").Parse(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.mock.updateTag) != fmt.Sprint([]any{id, stringTag("key", "value")}) {
		t.Fatal(f.mock.updateTag)
	}
}

func TestAmphora_deleteTag(t *testing.T) {
	f := newAmphoraFixture(t, "")
	id := uuid.New()
	if err := f.cli(DeleteTagCommand, "-i", id.String(), "key").Parse(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.mock.deleteTag) != fmt.Sprint([]any{id, "key"}) {
		t.Fatal(f.mock.deleteTag)
	}
}

// Beyond the Java suite: numeric tag values become LONG tags (TagTypeConverter).
func TestAmphora_tagConverterLongValues(t *testing.T) {
	for in, want := range map[string]amphora.Tag{
		"n=42":     {Key: "n", Value: "42", ValueType: amphora.TagLong},
		"n=-7":     {Key: "n", Value: "-7", ValueType: amphora.TagLong},
		"n=4.2":    {Key: "n", Value: "4.2", ValueType: amphora.TagString},
		"k=a=b":    {Key: "k", Value: "a=b", ValueType: amphora.TagString},
		"k=":       {Key: "k", Value: "", ValueType: amphora.TagString},
		"n=+1":     {Key: "n", Value: "+1", ValueType: amphora.TagLong},
		"n=999999": {Key: "n", Value: "999999", ValueType: amphora.TagLong},
	} {
		got, err := tagArg(in)
		if err != nil || got != want {
			t.Fatalf("%s: %v %v", in, got, err)
		}
	}
}
