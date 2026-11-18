// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package jc is the subset of JCommander 1.72 the Java CLI uses, with the same parsing rules, error
// messages and usage layout:
//
//   - an argument is an option only if it equals an option or command name (case sensitive, no
//     abbreviations); anything else, including unknown "-x" arguments, is a main parameter;
//   - arguments are trimmed and surrounding double quotes are removed; "@file" expands to the
//     file's non-comment lines; "--" sends the rest to the main parameter;
//   - a boolean option flips its current value; a list option collects every occurrence and, unless
//     it has its own splitter, splits each value at ",";
//   - the first non-option argument of a parser with commands selects the command, which parses the
//     remaining arguments;
//   - validation (skipped when a help option was given): required options, then the main parameter
//     (required, exact arity).
package jc

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// DefaultColumnSize is JCommander's default usage width.
const DefaultColumnSize = 79

// MaxColumnSize disables wrapping (Java Integer.MAX_VALUE).
const MaxColumnSize = math.MaxInt32

// Kind is the field type of an option.
type Kind int

const (
	// Bool options take no value and flip the field.
	Bool Kind = iota
	// Value options take one value; specifying them twice is an error.
	Value
	// List options take one value per occurrence and collect them.
	List
)

// Option is a @Parameter with names.
type Option struct {
	Names       []string
	Description string
	Required    bool
	Hidden      bool
	Help        bool
	Order       int // -1: none
	Kind        Kind
	Arity       int // -1: default
	// Split splits each value of a List option at "," (JCommander's CommaParameterSplitter); false is
	// the CLI's NoSplitter.
	Split bool
	// Default is shown in the usage as "Default: <Default>" when HasDefault is set and Help is not.
	Default    string
	HasDefault bool
	// Set converts and stores one value; name is the option name as typed. Errors are reported as
	// ParameterErrors with their message unchanged (JCommander converters throw ParameterException).
	Set func(name, value string) error
	// Flip toggles a Bool option.
	Flip func()
	// Reset clears a List option before its first value (replacing the field's initial value).
	Reset func()

	assigned bool
}

func (o *Option) names() string { return strings.Join(o.Names, ", ") }

func (o *Option) longestName() string {
	r := ""
	for _, n := range o.Names {
		if len(n) > len(r) {
			r = n
		}
	}
	return r
}

// Main is the main (positional) parameter.
type Main struct {
	Description string
	Required    bool
	Arity       int // -1: default
	List        bool
	// Set converts and stores one value.
	Set func(value string) error
	// Count returns the number of stored values (for the arity check).
	Count func() int
	// Reset clears a List main parameter before its first value.
	Reset func()

	assigned  bool
	first     bool
	single    string
	hasSingle bool
}

// Command is a sub-command with its own parser.
type Command struct {
	Name        string
	Description string
	Hidden      bool
	JC          *JC
}

// JC is one parser (a JCommander instance).
type JC struct {
	ProgramName string
	Options     []*Option
	Main        *Main
	Commands    []*Command
	// ColumnSize is the usage width; commands use the root's value.
	ColumnSize int

	parsedCommand    string
	helpWasSpecified bool
}

// ParameterError is JCommander's ParameterException; JC is the parser the error belongs to (set by
// the public Parse, like ParameterException.getJCommander()).
type ParameterError struct {
	Msg string
	JC  *JC
	// MissingCommand marks a MissingCommandException.
	MissingCommand bool
}

func (e *ParameterError) Error() string { return e.Msg }

// Message is the error's own message (for log rendering).
func (e *ParameterError) Message() string { return e.Msg }

func perr(format string, a ...any) error { return &ParameterError{Msg: fmt.Sprintf(format, a...)} }

// ParsedCommand is the name of the command found while parsing, or "".
func (j *JC) ParsedCommand() string { return j.parsedCommand }

// Command returns the command with the given name.
func (j *JC) Command(name string) *Command {
	for _, c := range j.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Parse parses args and validates the result.
func (j *JC) Parse(args ...string) error {
	if err := j.parse(true, args); err != nil {
		if pe, ok := err.(*ParameterError); ok {
			pe.JC = j
		}
		return err
	}
	return nil
}

func (j *JC) parse(validate bool, args []string) error {
	for _, m := range []*Main{j.Main} {
		if m != nil {
			m.first = true
		}
	}
	expanded, err := j.expandArgs(args)
	if err != nil {
		return err
	}
	if err := j.parseValues(expanded, validate); err != nil {
		return err
	}
	if validate {
		return j.validate()
	}
	return nil
}

func (j *JC) expandArgs(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "@") {
			lines, err := readAtFile(a[1:])
			if err != nil {
				return nil, err
			}
			out = append(out, lines...)
		} else {
			out = append(out, a)
		}
	}
	return out, nil
}

func readAtFile(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, perr("Could not read file %s: %v", name, err)
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := s.Text()
		if len(l) > 0 && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			out = append(out, l)
		}
	}
	return out, nil
}

// trim is JCommander's trim: Java String.trim plus removal of surrounding double quotes.
func trim(s string) string {
	r := strings.TrimFunc(s, func(c rune) bool { return c <= ' ' })
	if len(r) > 1 && strings.HasPrefix(r, "\"") && strings.HasSuffix(r, "\"") {
		r = r[1 : len(r)-1]
	}
	return r
}

func (j *JC) findOption(name string) *Option {
	for _, o := range j.Options {
		for _, n := range o.Names {
			if n == name {
				return o
			}
		}
	}
	return nil
}

func (j *JC) isOption(arg string) bool {
	if j.findOption(arg) != nil {
		return true
	}
	return j.Command(arg) != nil
}

func (j *JC) parseValues(args []string, validate bool) error {
	isDashDash := false
	for i := 0; i < len(args); {
		arg := args[i]
		a := trim(arg)
		args[i] = a
		cmd := j.Command(arg)
		increment := 1
		if !isDashDash && a != "--" && j.isOption(a) && cmd == nil {
			o := j.findOption(a)
			if o == nil {
				return perr("Unknown option: %s", arg)
			}
			if o.Kind == Bool && o.Arity == -1 {
				o.Flip()
				o.assigned = true
			} else {
				n, err := j.processFixedArity(args, i, o)
				if err != nil {
					return err
				}
				increment = n
			}
			if o.Help {
				j.helpWasSpecified = true
			}
		} else {
			switch {
			case arg == "--" && !isDashDash:
				isDashDash = true
			case len(j.Commands) == 0:
				if err := j.addMain(arg, a); err != nil {
					return err
				}
			default:
				if cmd == nil && validate {
					return &ParameterError{Msg: "Expected a command, got " + arg, MissingCommand: true}
				} else if cmd != nil {
					j.parsedCommand = cmd.Name
					if cmd.JC.ColumnSize == 0 {
						cmd.JC.ColumnSize = j.ColumnSize
					}
					return cmd.JC.parse(validate, append([]string(nil), args[i+1:]...))
				}
			}
		}
		i += increment
	}
	return nil
}

func (j *JC) processFixedArity(args []string, index int, o *Option) (int, error) {
	arity := o.Arity
	if arity == -1 {
		arity = 1
	}
	arg := args[index]
	switch {
	case arity == 0 && o.Kind == Bool:
		o.Flip()
		o.assigned = true
	case arity == 0:
		return 0, perr("Expected a value after parameter %s", arg)
	case index < len(args)-1:
		offset := 0
		if args[index+1] == "--" {
			offset = 1
		}
		if index+arity < len(args) {
			for k := 1; k <= arity; k++ {
				if err := j.addValue(o, arg, trim(args[index+k+offset]), k-1); err != nil {
					return 0, err
				}
			}
		} else {
			return 0, perr("Expected %d values after %s", arity, arg)
		}
	default:
		return 0, perr("Expected a value after parameter %s", arg)
	}
	return arity + 1, nil
}

func (j *JC) addValue(o *Option, name, value string, index int) error {
	if index == 0 && o.assigned && o.Kind != List {
		return perr("Can only specify option %s once.", name)
	}
	if o.Kind == List {
		if !o.assigned && o.Reset != nil {
			o.Reset()
		}
		parts := []string{value}
		if o.Split {
			parts = javaSplit(value, ",")
		}
		for _, p := range parts {
			if err := o.Set(name, p); err != nil {
				return err
			}
		}
	} else if err := o.Set(name, value); err != nil {
		return err
	}
	o.assigned = true
	return nil
}

func (j *JC) addMain(arg, value string) error {
	m := j.Main
	if m == nil {
		return perr("Was passed main parameter '%s' but no main parameter was defined in your arg class", arg)
	}
	if m.List {
		if m.first && m.Reset != nil {
			m.Reset()
		}
		m.first = false
	} else if m.hasSingle {
		return perr("Only one main parameter allowed but found several: \"%s\" and \"%s\"", m.single, value)
	}
	if err := m.Set(value); err != nil {
		return err
	}
	if !m.List {
		m.single, m.hasSingle = value, true
	}
	m.assigned = true
	return nil
}

func (j *JC) validate() error {
	if j.helpWasSpecified {
		return nil
	}
	var missing []string
	for _, o := range j.Options {
		if o.Required && !o.assigned {
			missing = append(missing, "["+strings.Join(o.Names, " | ")+"]")
		}
	}
	if len(missing) > 0 {
		what := "option is required: "
		if len(missing) > 1 {
			what = "options are required: "
		}
		return perr("The following %s%s", what, strings.Join(missing, ", "))
	}
	if m := j.Main; m != nil {
		if m.Required && !m.assigned {
			return perr("Main parameters are required (\"%s\")", m.Description)
		}
		if m.Arity != -1 && m.List && m.Count != nil {
			if n := m.Count(); n != m.Arity {
				return perr("There should be exactly %d main parameters but %d were found", m.Arity, n)
			}
		}
	}
	return nil
}

// javaSplit is Java's String.split with a literal separator: trailing empty strings are removed.
func javaSplit(s, sep string) []string {
	parts := strings.Split(s, sep)
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 && parts[0] == "" && s != "" {
		return nil
	}
	return parts
}

// Usage renders the usage like JCommander.usage(StringBuilder).
func (j *JC) Usage() string {
	var b strings.Builder
	j.usage(&b, "", j.columnSize())
	return b.String()
}

func (j *JC) columnSize() int {
	if j.ColumnSize == 0 {
		return DefaultColumnSize
	}
	return j.ColumnSize
}

func (j *JC) usage(out *strings.Builder, indent string, column int) {
	const descriptionIndent = 6
	indentCount := len(indent) + descriptionIndent
	name := j.ProgramName
	if name == "" {
		name = "<main class>"
	}
	main := indent + "Usage: " + name
	if len(j.Options) > 0 {
		main += " [options]"
	}
	if len(j.Commands) > 0 {
		main += indent + " [command] [command options]"
	}
	if j.Main != nil {
		main += " " + j.Main.Description
	}
	wrap(out, column, indentCount, main)
	out.WriteString("\n")

	var sorted []*Option
	for _, o := range j.Options {
		if !o.Hidden {
			sorted = append(sorted, o)
		}
	}
	sort.SliceStable(sorted, func(a, b int) bool { return compare(sorted[a], sorted[b]) < 0 })
	if len(sorted) > 0 {
		out.WriteString(indent + "  Options:\n")
	}
	for _, o := range sorted {
		mark := "  "
		if o.Required {
			mark = "* "
		}
		out.WriteString(indent + "  " + mark + o.names() + "\n")
		wrap(out, column, indentCount, strings.Repeat(" ", indentCount)+o.Description)
		if o.HasDefault && !o.Help {
			def := o.Default
			if def == "" {
				def = "<empty string>"
			}
			out.WriteString("\n" + strings.Repeat(" ", indentCount) + "Default: " + def)
		}
		out.WriteString("\n")
	}
	if len(j.Commands) > 0 {
		out.WriteString(indent + "  Commands:\n")
		for _, c := range j.Commands {
			if c.Hidden {
				continue
			}
			wrap(out, column, indentCount+descriptionIndent, indent+"    "+c.Name+"      "+c.Description)
			out.WriteString("\n")
			c.JC.ProgramName = c.Name
			c.JC.usage(out, indent+"      ", column)
			out.WriteString("\n")
		}
	}
}

// compare is JCommander's default parameter order: options with an order first (ascending), then the
// others by their longest name.
func compare(a, b *Option) int {
	switch {
	case a.Order != -1 && b.Order != -1:
		return a.Order - b.Order
	case a.Order != -1:
		return -1
	case b.Order != -1:
		return 1
	}
	return strings.Compare(a.longestName(), b.longestName())
}

// wrap is JCommander's wrapDescription: words (split at single spaces) are appended while they fit
// in max columns; a word that does not fit starts a new line indented by indent spaces.
func wrap(out *strings.Builder, max, indent int, description string) {
	words := strings.Split(description, " ")
	current := 0
	for i, word := range words {
		if len(word) > max || current+1+len(word) <= max {
			out.WriteString(word)
			current += len(word)
			if i != len(words)-1 {
				out.WriteString(" ")
				current++
			}
		} else {
			out.WriteString("\n" + strings.Repeat(" ", indent) + word + " ")
			current = indent + 1 + len(word)
		}
	}
}
