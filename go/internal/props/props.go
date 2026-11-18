// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package props reads the CLI's message bundles (Java .properties files, embedded unchanged from
// src/main/resources) and formats their messages like java.text.MessageFormat.
package props

import (
	"embed"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
)

// Bundle names (Java *_MESSAGE_BUNDLE constants).
const (
	CsCli         = "CsCliMessageBundle"
	Amphora       = "AmphoraMessageBundle"
	Castor        = "CastorMessageBundle"
	Ephemeral     = "EphemeralMessageBundle"
	Thymus        = "ThymusMessageBundle"
	Configuration = "ConfigurationMessageBundle"
	Login         = "LoginMessageBundle"
)

//go:embed bundles/*.properties
var files embed.FS

var (
	mu    sync.Mutex
	cache = map[string]map[string]string{}
)

// Get returns the message of key in bundle. A missing key panics like Java's MissingResourceException:
// it is a programming error.
func Get(bundle, key string) string {
	mu.Lock()
	defer mu.Unlock()
	m, ok := cache[bundle]
	if !ok {
		b, err := files.ReadFile("bundles/" + bundle + ".properties")
		if err != nil {
			panic(fmt.Sprintf("Can't find bundle for base name %s", bundle))
		}
		m = Parse(string(b))
		cache[bundle] = m
	}
	v, ok := m[key]
	if !ok {
		panic(fmt.Sprintf("Can't find resource for bundle %s, key %s", bundle, key))
	}
	return v
}

// Format is MessageFormat.format(Get(bundle, key), args...).
func Format(bundle, key string, args ...any) string {
	return MessageFormat(Get(bundle, key), args...)
}

// Parse reads a .properties document with the rules of java.util.Properties.load: comments (# and !),
// key/value separators (=, : or white space), line continuations and backslash escapes.
func Parse(doc string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(doc, "\r\n", "\n"), "\r", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// join continuation lines: an odd number of trailing backslashes continues the logical line
		for continues(line) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimLeft(lines[i], " \t\f")
		}
		if continues(line) { // continuation at end of file
			line = line[:len(line)-1]
		}
		key, value := split(line)
		out[unescape(key)] = unescape(value)
	}
	return out
}

func continues(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// split separates key and value at the first unescaped '=', ':' or white space.
func split(line string) (string, string) {
	keyLen, valueStart, hasSep := 0, len(line), false
	for keyLen < len(line) {
		c := line[keyLen]
		if c == '\\' {
			keyLen += 2
			continue
		}
		if c == '=' || c == ':' {
			valueStart, hasSep = keyLen+1, true
			break
		}
		if c == ' ' || c == '\t' || c == '\f' {
			valueStart = keyLen + 1
			break
		}
		keyLen++
	}
	if keyLen > len(line) {
		keyLen = len(line)
	}
	for valueStart < len(line) {
		c := line[valueStart]
		if c != ' ' && c != '\t' && c != '\f' {
			if !hasSep && (c == '=' || c == ':') {
				hasSep = true
			} else {
				break
			}
		}
		valueStart++
	}
	return line[:keyLen], line[valueStart:]
}

func unescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// MessageFormat formats pattern like java.text.MessageFormat.format with the default locale (en):
// '{n}' is replaced by args[n], quotes delimit literal text (” is a single quote), integers are grouped
// by thousands, nil is "null" and other values use their String form (Java toString).
func MessageFormat(pattern string, args ...any) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\'':
			if i+1 < len(pattern) && pattern[i+1] == '\'' {
				b.WriteByte('\'')
				i++
			} else {
				quoted = !quoted
			}
		case c == '{' && !quoted:
			end := strings.IndexByte(pattern[i:], '}')
			if end < 0 {
				panic("Unmatched braces in the pattern.")
			}
			spec := pattern[i+1 : i+end]
			idx, rest, _ := strings.Cut(spec, ",")
			n, err := strconv.Atoi(strings.TrimSpace(idx))
			if err != nil {
				panic(fmt.Sprintf("can't parse argument number: %s", idx))
			}
			if n < len(args) {
				b.WriteString(formatArg(args[n], strings.TrimSpace(rest)))
			} else {
				b.WriteString("{" + idx + "}")
			}
			i += end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func formatArg(v any, style string) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case int:
		return group(strconv.FormatInt(int64(x), 10))
	case int32:
		return group(strconv.FormatInt(int64(x), 10))
	case int64:
		return group(strconv.FormatInt(x, 10))
	case *big.Int:
		return group(x.String())
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	}
	_ = style
	return fmt.Sprint(v)
}

// group inserts ',' every three digits (NumberFormat.getInstance() of the en locale).
func group(digits string) string {
	neg := strings.HasPrefix(digits, "-")
	if neg {
		digits = digits[1:]
	}
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Slf4j formats like an slf4j logger call: each "{}" is replaced by the next argument; "{n}" stays as is.
func Slf4j(pattern string, args ...any) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+2 < len(pattern) && pattern[i+1] == '{' && pattern[i+2] == '}' {
			b.WriteString("{}")
			i += 2
			continue
		}
		if pattern[i] == '{' && i+1 < len(pattern) && pattern[i+1] == '}' && n < len(args) {
			b.WriteString(toString(args[n]))
			n++
			i++
			continue
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}

func toString(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	}
	return fmt.Sprint(v)
}

// UnescapeJava is commons-text StringEscapeUtils.unescapeJava for the escapes that occur in the CLI's texts.
func UnescapeJava(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '\\', '\'', '"':
			b.WriteByte(s[i])
		case 'u':
			if i+4 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			b.WriteString("\\u")
		default:
			if s[i] >= '0' && s[i] <= '7' {
				j := i
				for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
					j++
				}
				v, _ := strconv.ParseUint(s[i:j], 8, 32)
				b.WriteRune(rune(v))
				i = j - 1
				continue
			}
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
