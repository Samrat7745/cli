// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package jc

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type parsed struct {
	flag   bool
	single string
	list   []string
	main   []string
}

func parser(p *parsed) *JC {
	return &JC{ProgramName: "prog",
		Options: []*Option{
			{Names: []string{"-f", "--flag"}, Order: -1, Kind: Bool, Arity: -1, Flip: func() { p.flag = !p.flag }},
			{Names: []string{"-s"}, Order: -1, Kind: Value, Arity: -1, Set: func(_, v string) error { p.single = v; return nil }},
			{Names: []string{"-l"}, Order: -1, Kind: List, Arity: -1, Split: true,
				Set: func(_, v string) error { p.list = append(p.list, v); return nil }},
		},
		Main: &Main{Description: "ARGS", Arity: -1, List: true, Set: func(v string) error { p.main = append(p.main, v); return nil }}}
}

func TestParsingRules(t *testing.T) {
	at := filepath.Join(t.TempDir(), "args")
	_ = os.WriteFile(at, []byte("# comment\n-s\nfrom-file\n"), 0o644)
	var p parsed
	err := parser(&p).Parse("-x", " \"quoted\" ", "-f", "-f", "-f", "-l", "a,b,,c,", "-l", "d", "@"+at, "--", "-s")
	if err != nil {
		t.Fatal(err)
	}
	if !p.flag || p.single != "from-file" || !slices.Equal(p.list, []string{"a", "b", "", "c", "d"}) ||
		!slices.Equal(p.main, []string{"-x", "quoted", "-s"}) {
		t.Fatalf("%+v", p)
	}
}

func TestErrors(t *testing.T) {
	for args, want := range map[string][]string{
		"Can only specify option -s once.":                                               []string{"-s", "a", "-s", "b"},
		"Expected a value after parameter -s":                                            []string{"-s"},
		"Could not read file /nonexistent: open /nonexistent: no such file or directory": []string{"@/nonexistent"},
	} {
		var p parsed
		err := parser(&p).Parse(want...)
		if err == nil || err.Error() != args {
			t.Errorf("%v: got %v want %q", want, err, args)
		}
		if pe, ok := err.(*ParameterError); !ok || pe.JC == nil {
			t.Errorf("%v: ParameterError without parser", want)
		}
	}
}

func TestCommandsAndValidation(t *testing.T) {
	var got []string
	sub := &JC{Options: []*Option{{Names: []string{"-i"}, Required: true, Order: -1, Kind: Value, Arity: -1,
		Set: func(_, v string) error { got = append(got, v); return nil }}},
		Main: &Main{Description: "ID", Required: true, Arity: 1, List: true,
			Set: func(v string) error { got = append(got, v); return nil }, Count: func() int { return len(got) - 1 }}}
	root := func() *JC { return &JC{ProgramName: "cs", Commands: []*Command{{Name: "cmd", JC: sub}}} }
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"other"}, "Expected a command, got other"},
		{[]string{"cmd", "x"}, "The following option is required: [-i]"},
		{[]string{"cmd", "-i", "v"}, `Main parameters are required ("ID")`},
		{[]string{"cmd", "-i", "v", "a", "b"}, "There should be exactly 1 main parameters but 2 were found"},
	} {
		got = nil
		sub.Options[0].assigned, sub.Main.assigned, sub.Main.hasSingle = false, false, false
		err := root().Parse(c.args...)
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: got %v want %q", c.args, err, c.want)
		}
	}
	got = nil
	sub.Options[0].assigned, sub.Main.assigned = false, false
	r := root()
	if err := r.Parse("cmd", "-i", "v", "a"); err != nil || r.ParsedCommand() != "cmd" {
		t.Fatal(err, r.ParsedCommand())
	}
}

func TestWrap(t *testing.T) {
	j := &JC{ProgramName: "p", ColumnSize: 20, Options: []*Option{{Names: []string{"--opt"}, Order: -1, Kind: Bool, Arity: -1,
		Description: "one two three four five six", Flip: func() {}}}}
	// JCommander keeps the space before a line break.
	want := "Usage: p [options]\n  Options:\n    --opt\n      one two three \n      four five six\n"
	if got := j.Usage(); got != want {
		t.Fatalf("%q", got)
	}
}
