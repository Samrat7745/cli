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
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"

	amphora "github.com/Samrat7745/amphora/go/client"
	"github.com/google/uuid"

	"github.com/Samrat7745/cli/go/internal/config"
	"github.com/Samrat7745/cli/go/internal/javautil"
	"github.com/Samrat7745/cli/go/internal/jc"
	"github.com/Samrat7745/cli/go/internal/props"
)

// AmphoraClientName is the amphora client.
const AmphoraClientName = "amphora"

// Amphora command names.
const (
	CreateSecretCommand  = "create-secret"
	GetSecretCommand     = "get-secret"
	ListSecretsCommand   = "get-secrets"
	CreateTagCommand     = "create-tag"
	GetTagsCommand       = "get-tags"
	OverwriteTagsCommand = "overwrite-tags"
	GetTagCommand        = "get-tag"
	UpdateTagCommand     = "update-tag"
	DeleteTagCommand     = "delete-tag"
	DeleteSecretsCommand = "delete-secrets"
)

// AmphoraFactory creates the Amphora client (AmphoraClientFactory); nil uses the configuration.
type AmphoraFactory func() (amphora.Client, error)

func am(key string, args ...any) string { return props.Format(props.Amphora, key, args...) }

// AmphoraClient registers the amphora client.
func AmphoraClient(factory AmphoraFactory) Client {
	return Client{Name: AmphoraClientName, Description: props.Get(props.Amphora, "client-description"),
		New: func(e *config.Env, args []string, debug bool) *ClientCli {
			return NewAmphoraCli(e, args, debug, factory)
		}}
}

// tagArg is TagTypeConverter: "<KEY>=<VALUE>" split at the first "="; numeric values are LONG tags.
func tagArg(value string) (amphora.Tag, error) {
	k, v, ok := strings.Cut(value, "=")
	if !ok {
		return amphora.Tag{}, &jc.ParameterError{Msg: props.Get(props.Amphora, "tag.conversion.exception.format")}
	}
	t := amphora.Tag{Key: k, Value: v, ValueType: amphora.TagString}
	if _, err := javautil.ParseLong(v); err == nil {
		t.ValueType = amphora.TagLong
	}
	return t, nil
}

func uuidOption(target *uuid.UUID, required bool, descKey string, order int) *jc.Option {
	return &jc.Option{Names: []string{"-i", "--secret-id"}, Description: props.Get(props.Amphora, descKey),
		Required: required, Order: order, Kind: jc.Value, Arity: -1,
		Set: func(_, v string) error {
			s, err := parseUUIDArg(v)
			if err == nil {
				*target = uuid.MustParse(s)
			}
			return err
		}}
}

func uuidMain(target *[]uuid.UUID, descKey string, required bool, arity int) *jc.Main {
	return &jc.Main{Description: props.Get(props.Amphora, descKey), Required: required, Arity: arity, List: true,
		Reset: func() { *target = nil },
		Set: func(v string) error {
			s, err := parseUUIDArg(v)
			if err == nil {
				*target = append(*target, uuid.MustParse(s))
			}
			return err
		},
		Count: func() int { return len(*target) }}
}

func tagMain(target *[]amphora.Tag, descKey string, required bool, arity int) *jc.Main {
	return &jc.Main{Description: props.Get(props.Amphora, descKey), Required: required, Arity: arity, List: true,
		Reset: func() { *target = nil },
		Set: func(v string) error {
			t, err := tagArg(v)
			if err == nil {
				*target = append(*target, t)
			}
			return err
		},
		Count: func() int { return len(*target) }}
}

// NewAmphoraCli is AmphoraClientCli: the amphora commands.
func NewAmphoraCli(e *config.Env, args []string, debug bool, factory AmphoraFactory) *ClientCli {
	c := &ClientCli{Env: e, Name: AmphoraClientName, Args: args, Debug: debug}
	newRunner := func(run func(r *amphoraRunner) error) func() (Runner, error) {
		return func() (Runner, error) {
			r, err := newAmphoraRunner(e, factory)
			if err != nil {
				return nil, err
			}
			return runFunc(func() error { return run(r) }), nil
		}
	}

	// create-secret
	{
		var help bool
		var secrets []string
		var id uuid.UUID
		var tags []amphora.Tag
		c.Commands = append(c.Commands, &ClientCommand{Name: CreateSecretCommand,
			Description: props.Get(props.Amphora, "create.command-description"),
			Main: &jc.Main{Description: props.Get(props.Amphora, "create.parameter.secret-description"), Arity: -1,
				List: true, Reset: func() { secrets = nil },
				Set:   func(v string) error { secrets = append(secrets, v); return nil },
				Count: func() int { return len(secrets) }},
			Options: []*jc.Option{
				hiddenHelp(&help),
				uuidOption(&id, false, "create.option.secretid-description", 1),
				{Names: []string{"-t", "--tag"}, Description: props.Get(props.Amphora, "create.option.tag-description"),
					Order: 2, Kind: jc.List, Arity: -1,
					Set: func(_, v string) error {
						t, err := tagArg(v)
						if err == nil {
							tags = append(tags, t)
						}
						return err
					}},
			},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				if id == uuid.Nil {
					id = uuid.New()
				}
				data, err := r.secretsArg(secrets)
				if err != nil {
					return err
				}
				res, err := r.client.CreateSecret(context.Background(), amphora.Secret{SecretID: id, Tags: tags, Data: data})
				if err != nil {
					return runnerErr("Failed to upload secret.", err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "create.log.success"), res)
				r.env.Println(res.String())
				return nil
			})})
	}
	// get-secret
	{
		var help bool
		var ids []uuid.UUID
		c.Commands = append(c.Commands, &ClientCommand{Name: GetSecretCommand,
			Description: props.Get(props.Amphora, "get.command-description"),
			Main:        uuidMain(&ids, "get.parameter.secretid-description", true, 1),
			Options:     []*jc.Option{hiddenHelp(&help)},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				s, err := r.client.GetSecret(context.Background(), ids[0])
				if err != nil {
					return runnerErr("Failed fetching secret.", err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "get.log.success"), s.SecretID)
				r.env.Println(SecretToString(s))
				return nil
			})})
	}
	// get-secrets
	{
		var help, idsOnly bool
		var filters []string
		var sortBy *string
		c.Commands = append(c.Commands, &ClientCommand{Name: ListSecretsCommand,
			Description: props.Get(props.Amphora, "list.command-description"),
			Options: []*jc.Option{
				hiddenHelp(&help),
				{Names: []string{"-l", "--list-ids-only"}, Description: props.Get(props.Amphora, "list.option.ids-only"),
					Order: -1, Kind: jc.Bool, Arity: -1, Default: "false", HasDefault: true,
					Flip: func() { idsOnly = !idsOnly }},
				{Names: []string{"-f", "--tagfilter"}, Description: props.Get(props.Amphora, "list.option.tag-filter-description"),
					Order: 2, Kind: jc.List, Arity: -1, Split: true,
					Set: func(_, v string) error { filters = append(filters, v); return nil }},
				{Names: []string{"-s", "--sortBy"}, Description: props.Get(props.Amphora, "list.option.sort-by-description"),
					Order: 3, Kind: jc.Value, Arity: -1,
					Set: func(_, v string) error { sortBy = &v; return nil }},
			},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				sort, err := sorting(sortBy)
				if err != nil {
					return err
				}
				tf, err := tagFilters(r, filters)
				if err != nil {
					return err
				}
				res, err := r.client.GetSecrets(context.Background(), tf, sort)
				if err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "list.log.success"), len(res))
				r.env.Println(MetadataListToString(res, idsOnly))
				return nil
			})})
	}
	// create-tag
	{
		var help bool
		var id uuid.UUID
		var tag []amphora.Tag
		c.Commands = append(c.Commands, &ClientCommand{Name: CreateTagCommand,
			Description: props.Get(props.Amphora, "create-tag.command-description"),
			Main:        tagMain(&tag, "create-tag.parameter.tag-description", true, 1),
			Options:     []*jc.Option{hiddenHelp(&help), uuidOption(&id, true, "create-tag.option.secret-id-description", -1)},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				if err := r.client.CreateTag(context.Background(), id, tag[0]); err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "create-tag.log.success"), tag[0].Key, tag[0].Value, id)
				r.env.Println(props.Get(props.Amphora, "create-tag.print.success"))
				return nil
			})})
	}
	// get-tags
	{
		var help bool
		var ids []uuid.UUID
		c.Commands = append(c.Commands, &ClientCommand{Name: GetTagsCommand,
			Description: props.Get(props.Amphora, "get-tags.command-description"),
			Main:        uuidMain(&ids, "get-tags.parameter.secret-id-description", true, 1),
			Options:     []*jc.Option{hiddenHelp(&help)},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				tags, err := r.client.GetTags(context.Background(), ids[0])
				if err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "get-tags.log.success"), len(tags))
				r.env.Println(TagsToString(tags))
				return nil
			})})
	}
	// overwrite-tags
	{
		var help bool
		var id uuid.UUID
		var tags []amphora.Tag
		main := tagMain(&tags, "overwrite-tags.parameter.tag-description", false, -1)
		c.Commands = append(c.Commands, &ClientCommand{Name: OverwriteTagsCommand,
			Description: props.Get(props.Amphora, "overwrite-tags.command-description"),
			Main:        main,
			Options:     []*jc.Option{hiddenHelp(&help), uuidOption(&id, true, "overwrite-tags.option.secret-id-description", -1)},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				if err := r.client.OverwriteTags(context.Background(), id, tags); err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "overwrite-tags.log.success"), id)
				r.env.Println(props.Get(props.Amphora, "overwrite-tags.print.success"))
				return nil
			})})
	}
	// get-tag, delete-tag (secret id option + tag key)
	for _, def := range []struct {
		name, descKey, optKey, mainKey string
		run                            func(r *amphoraRunner, id uuid.UUID, key string) error
	}{
		{GetTagCommand, "get-tag.command-description", "get-tag.option.secret-id-description", "get-tag.parameter.tag-key-description",
			func(r *amphoraRunner, id uuid.UUID, key string) error {
				t, err := r.client.GetTag(context.Background(), id, key)
				if err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "get-tag.log.success"), t)
				r.env.Println(TagToString(t))
				return nil
			}},
		{UpdateTagCommand, "", "", "", nil},
		{DeleteTagCommand, "delete-tag.command-description", "delete-tag.option.secret-id-description", "delete-tag.parameter.tag-key-description",
			func(r *amphoraRunner, id uuid.UUID, key string) error {
				if err := r.client.DeleteTag(context.Background(), id, key); err != nil {
					return runnerErr(err.Error(), err)
				}
				r.env.Log.Debugf(props.Get(props.Amphora, "delete-tag.log.success"))
				r.env.Println(props.Get(props.Amphora, "delete-tag.print.success"))
				return nil
			}},
	} {
		if def.name == UpdateTagCommand {
			var help bool
			var id uuid.UUID
			var tag []amphora.Tag
			c.Commands = append(c.Commands, &ClientCommand{Name: UpdateTagCommand,
				Description: props.Get(props.Amphora, "update-tag.command-description"),
				Main:        tagMain(&tag, "update-tag.parameter.tag-description", true, 1),
				Options:     []*jc.Option{hiddenHelp(&help), uuidOption(&id, true, "update-tag.option.secret-id-description", -1)},
				NewRunner: newRunner(func(r *amphoraRunner) error {
					if err := r.client.UpdateTag(context.Background(), id, tag[0]); err != nil {
						return runnerErr(err.Error(), err)
					}
					r.env.Log.Debugf(props.Get(props.Amphora, "update-tag.log.success"), tag[0].Key, tag[0].Value, id)
					r.env.Println(props.Get(props.Amphora, "update-tag.print.success"))
					return nil
				})})
			continue
		}
		def := def
		var help bool
		var id uuid.UUID
		var key string
		c.Commands = append(c.Commands, &ClientCommand{Name: def.name,
			Description: props.Get(props.Amphora, def.descKey),
			Main: &jc.Main{Description: props.Get(props.Amphora, def.mainKey), Required: true, Arity: -1,
				Set: func(v string) error { key = v; return nil }},
			Options:   []*jc.Option{hiddenHelp(&help), uuidOption(&id, true, def.optKey, -1)},
			NewRunner: newRunner(func(r *amphoraRunner) error { return def.run(r, id, key) })})
	}
	// delete-secrets
	{
		var help bool
		var ids []uuid.UUID
		c.Commands = append(c.Commands, &ClientCommand{Name: DeleteSecretsCommand,
			Description: props.Get(props.Amphora, "delete-secret.command-description"),
			Main:        uuidMain(&ids, "delete-secret.parameter.secret-id-description", false, -1),
			Options:     []*jc.Option{hiddenHelp(&help)},
			NewRunner: newRunner(func(r *amphoraRunner) error {
				if len(ids) == 0 {
					r.env.Println(props.Get(props.Amphora, "delete-secret.info.secret-ids"))
					read, err := readUUIDs(r.env)
					if err != nil {
						return javautil.IllegalArgument(props.Get(props.Amphora, "delete-secret.failure.secret-ids"), err)
					}
					ids = read
				}
				return r.deleteSecrets(ids)
			})})
	}
	// The command order of AmphoraClientCli.
	order := []string{CreateSecretCommand, GetSecretCommand, ListSecretsCommand, CreateTagCommand, GetTagsCommand,
		OverwriteTagsCommand, GetTagCommand, UpdateTagCommand, DeleteTagCommand, DeleteSecretsCommand}
	byName := map[string]*ClientCommand{}
	for _, cmd := range c.Commands {
		byName[cmd.Name] = cmd
	}
	c.Commands = c.Commands[:0]
	for _, n := range order {
		c.Commands = append(c.Commands, byName[n])
	}
	return c
}

type runFunc func() error

func (f runFunc) Run() error { return f() }

type amphoraRunner struct {
	env    *config.Env
	client amphora.Client
}

// newAmphoraRunner is the AmphoraClientCliCommandRunner constructor: it loads the configuration and the
// tokens and creates the client.
func newAmphoraRunner(e *config.Env, factory AmphoraFactory) (*amphoraRunner, error) {
	cfg, err := e.Instance()
	if err != nil {
		return nil, err
	}
	tokens, err := tokensByBaseURL(e)
	if err != nil {
		return nil, err
	}
	byService := map[string]string{}
	for base, t := range tokens {
		p, err := providerFor(cfg, base)
		if err != nil {
			return nil, err
		}
		byService[normalizeServiceURL(p.AmphoraURL())] = t.IDToken
	}
	var cl amphora.Client
	if factory != nil {
		cl, err = factory()
	} else {
		o := amphora.Options{Prime: cfg.GetPrime(), R: cfg.GetR(), RInv: cfg.GetRInv(),
			NoSSLValidation: cfg.IsNoSSLValidation()}
		for _, p := range cfg.Providers {
			o.ServiceAddresses = append(o.ServiceAddresses, p.AmphoraURL())
		}
		if tokens != nil {
			o.BearerTokenProvider = func(addr string) string { return byService[normalizeServiceURL(addr)] }
		}
		if !cfg.IsNoSSLValidation() {
			o.TrustedCertificates = cfg.TrustedCertificates
		}
		cl, err = amphora.New(o)
	}
	if err != nil {
		return nil, runnerErr(props.Get(props.Amphora, "client-instantiation-failed"), err)
	}
	return &amphoraRunner{env: e, client: cl}, nil
}

// secretsArg is CreateSecretAmphoraClientCliCommandConfig.getSecrets: the arguments, or the lines of
// stdin if there are none; empty values are dropped.
func (r *amphoraRunner) secretsArg(secrets []string) ([]*big.Int, error) {
	if len(secrets) == 0 {
		r.env.Println(props.Get(props.Amphora, "create.info.insert-secrets"))
		data, err := r.env.ReadAll()
		if err == nil && data == "" {
			err = javautil.IllegalArgument("No input provided", nil)
		}
		if err != nil {
			return nil, javautil.IllegalArgument(props.Get(props.Amphora, "create.failure.read-secrets"), err)
		}
		for _, l := range javaSplitLines(data) {
			secrets = append(secrets, strings.TrimSpace(l))
		}
	}
	out := []*big.Int{}
	for _, s := range secrets {
		if s == "" {
			continue
		}
		v, err := javautil.ParseBigInteger(s)
		if err != nil {
			return nil, javautil.NumberFormat(err.Error())
		}
		out = append(out, v)
	}
	return out, nil
}

// javaSplitLines is data.split(System.lineSeparator()) on Linux.
func javaSplitLines(data string) []string {
	parts := strings.Split(data, "\n")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func readUUIDs(e *config.Env) ([]uuid.UUID, error) {
	data, err := e.ReadAll()
	if err == nil && data == "" {
		err = javautil.IllegalArgument("No input provided", nil)
	}
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for _, l := range javaSplitLines(data) {
		id, err := javautil.ParseUUID(strings.TrimSpace(l))
		if err != nil {
			return nil, javautil.IllegalArgument(err.Error(), nil)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// deleteSecrets deletes all ids in parallel, printing a line per id, and fails if any delete failed.
func (r *amphoraRunner) deleteSecrets(ids []uuid.UUID) error {
	var mu sync.Mutex
	failed := 0
	sem := make(chan struct{}, max(1, runtime.NumCPU()-1))
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id uuid.UUID) {
			defer wg.Done()
			defer func() { <-sem }()
			err := r.client.DeleteSecret(context.Background(), id)
			mu.Lock()
			defer mu.Unlock()
			var ce *amphora.ClientError
			switch {
			case err == nil:
				r.env.Println(am("delete-secret.print.success-id", id))
				r.env.Log.Debugf(props.Get(props.Amphora, "delete-secret.print.success-id"), id)
				return
			case errors.As(err, &ce):
				line := am("delete-secret.print.fail-id", id, err.Error())
				r.env.Println(line)
				r.env.Log.Debug(line, err)
			default:
				line := am("delete-secret.print.fail-unexp-id", id, "Unexpected exception "+err.Error())
				r.env.Println(line)
				r.env.Log.Debug(line, err)
			}
			failed++
		}(id)
	}
	wg.Wait()
	if failed > 0 {
		return runnerErr("Failed to delete secrets", nil)
	}
	r.env.Println(props.Get(props.Amphora, "delete-secret.print.success-all"))
	r.env.Log.Debugf(props.Get(props.Amphora, "delete-secret.print.success-all"))
	return nil
}

// sorting is ListSecretsAmphoraClientCliCommandRunner.getSorting: "<KEY>:ASC|DESC".
func sorting(sortBy *string) (*amphora.Sort, error) {
	if sortBy == nil {
		return nil, nil
	}
	parts := strings.Split(*sortBy, ":")
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) != 2 || (parts[1] != "ASC" && parts[1] != "DESC") {
		return nil, runnerErr(am("list.failure.invalid-sort-format", *sortBy), nil)
	}
	return &amphora.Sort{Property: parts[0], Desc: parts[1] == "DESC"}, nil
}

var tagFilterPattern = regexp.MustCompile(`^([\w\-.%]+?)(:|<|>)([\w\-.%]+?)$`)

// ParseTagFilter is TagFilter.fromString: key, operator (":", "<" or ">") and value, URL decoded.
func ParseTagFilter(s string) (amphora.TagFilter, error) {
	m := tagFilterPattern.FindStringSubmatch(s)
	if m == nil {
		return amphora.TagFilter{}, fmt.Errorf("Illegal tag filter format: %s", s)
	}
	k, err := url.QueryUnescape(m[1])
	if err != nil {
		return amphora.TagFilter{}, err
	}
	v, err := url.QueryUnescape(m[3])
	if err != nil {
		return amphora.TagFilter{}, err
	}
	return amphora.TagFilter{Key: k, Operator: m[2], Value: v}, nil
}

func tagFilters(r *amphoraRunner, filters []string) ([]amphora.TagFilter, error) {
	out := []amphora.TagFilter{}
	for _, f := range filters {
		tf, err := ParseTagFilter(f)
		if err != nil {
			r.env.Log.Debugf(props.Get(props.Amphora, "list.failure.invalid-tag-filter"), err.Error())
			return nil, runnerErr(am("list.failure.invalid-tag-filter", f), nil)
		}
		out = append(out, tf)
	}
	return out, nil
}

// ---- SecretPrinter ----

// MetadataListToString prints secret ids, each followed by its tags unless idsOnly.
func MetadataListToString(list []amphora.Metadata, idsOnly bool) string {
	var b strings.Builder
	for i, m := range list {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.SecretID)
		if !idsOnly {
			b.WriteString("\n" + TagsToString(m.Tags))
		}
	}
	return b.String()
}

// SecretToString prints the data ("[1, 2]") and the tags.
func SecretToString(s *amphora.Secret) string {
	return javautil.ListString(s.Data) + "\n" + TagsToString(s.Tags)
}

// TagsToString prints one "\t<key> -> <value>" line per tag.
func TagsToString(tags []amphora.Tag) string {
	if tags == nil {
		return ""
	}
	var b strings.Builder
	for _, t := range tags {
		b.WriteString(TagToString(&t) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// TagToString prints "\t<key> -> <value>".
func TagToString(t *amphora.Tag) string {
	if t == nil {
		return ""
	}
	return "\t" + t.Key + " -> " + t.Value
}
