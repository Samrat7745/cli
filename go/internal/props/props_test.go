// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package props

import (
	"os"
	"path/filepath"
	"testing"
)

// The embedded bundles must stay identical to the Java resources they were copied from.
func TestBundlesMatchJavaResources(t *testing.T) {
	entries, err := files.ReadDir("bundles")
	if err != nil || len(entries) != 7 {
		t.Fatal(err, len(entries))
	}
	for _, e := range entries {
		embedded, _ := files.ReadFile("bundles/" + e.Name())
		java, err := os.ReadFile(filepath.Join("..", "..", "..", "src", "main", "resources", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if string(embedded) != string(java) {
			t.Errorf("%s differs from src/main/resources", e.Name())
		}
	}
}

func TestParseJavaProperties(t *testing.T) {
	m := Parse("# comment\n! comment\na=1\nb : 2\nc 3\nd=x\\\n   y\\n\\tz\ne=\\u0041\\=\\:\nf\n  g = h  \n")
	want := map[string]string{"a": "1", "b": "2", "c": "3", "d": "xy\n\tz", "e": "A=:", "f": "", "g": "h  "}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s: got %q want %q", k, m[k], v)
		}
	}
	if Get(Amphora, "create-tag.print.success") != "Tag created" {
		t.Fatal(Get(Amphora, "create-tag.print.success"))
	}
}

func TestMessageFormat(t *testing.T) {
	for _, c := range []struct {
		pattern string
		args    []any
		want    string
	}{
		{"Invalid UUID {0}.", []any{"x"}, "Invalid UUID x."},
		{"status code: {0}, message: {1}.", []any{500, "m"}, "status code: 500, message: m."},
		{"[{0}]", []any{1234567}, "[1,234,567]"},
		{"separated by ';' [{0}]", []any{""}, "separated by ; []"},
		{"it''s {0}", []any{nil}, "it's null"},
		{"'{0}' {1}", []any{"a", "b"}, "{0} b"},
	} {
		if got := MessageFormat(c.pattern, c.args...); got != c.want {
			t.Errorf("%q: got %q want %q", c.pattern, got, c.want)
		}
	}
}

func TestUnescapeJava(t *testing.T) {
	if got := UnescapeJava(`a\tb\n\"c\" \u0041 \101 \\`); got != "a\tb\n\"c\" A A \\" {
		t.Fatalf("%q", got)
	}
}
