// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package logx is the CLI's logger. It reproduces the Java CLI's log4j setup (log4j.properties): a
// console appender on stdout with the pattern "%m%n", root level INFO, DEBUG with --debug. An error
// logged with a cause is followed by the cause chain ("Caused by: ..."), like log4j prints a throwable
// (without the Java stack frames).
package logx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Level is a log4j level.
type Level int

const (
	Debug Level = iota
	Info
	Error
)

// Event is one logged message (log4j LoggingEvent: level and rendered message, without the throwable).
type Event struct {
	Level   Level
	Message string
}

// Logger writes to Out and records every event that passes the level.
type Logger struct {
	mu     sync.Mutex
	Out    io.Writer
	level  Level
	events []Event
}

// New returns a logger on out with level INFO.
func New(out io.Writer) *Logger {
	if out == nil {
		out = os.Stdout
	}
	return &Logger{Out: out, level: Info}
}

// SetLevel changes the root level (Java Logger.getRootLogger().setLevel).
func (l *Logger) SetLevel(lv Level) {
	l.mu.Lock()
	l.level = lv
	l.mu.Unlock()
}

// IsDebugEnabled reports whether debug messages are logged.
func (l *Logger) IsDebugEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level <= Debug
}

// Events returns the recorded events.
func (l *Logger) Events() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Event(nil), l.events...)
}

// Log returns the rendered messages joined by "\n" (the Java tests' LoggingRule.getLog()).
func (l *Logger) Log() string {
	ev := l.Events()
	msgs := make([]string, len(ev))
	for i, e := range ev {
		msgs[i] = e.Message
	}
	return strings.Join(msgs, "\n")
}

func (l *Logger) log(lv Level, msg string, cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lv < l.level {
		return
	}
	l.events = append(l.events, Event{lv, msg})
	var b strings.Builder
	b.WriteString(msg)
	b.WriteByte('\n')
	if cause != nil {
		b.WriteString(Throwable(cause))
	}
	_, _ = io.WriteString(l.Out, b.String())
}

// Debugf logs an slf4j-style message ("{}" placeholders) at DEBUG.
func (l *Logger) Debugf(pattern string, args ...any) { l.log(Debug, slf4j(pattern, args...), nil) }

// Debug logs msg with an optional cause at DEBUG.
func (l *Logger) Debug(msg string, cause error) { l.log(Debug, msg, cause) }

// Info logs msg at INFO.
func (l *Logger) Info(msg string) { l.log(Info, msg, nil) }

// Error logs msg with an optional cause at ERROR.
func (l *Logger) Error(msg string, cause error) { l.log(Error, msg, cause) }

// Errorf logs an slf4j-style message at ERROR.
func (l *Logger) Errorf(pattern string, args ...any) { l.log(Error, slf4j(pattern, args...), nil) }

// Messenger is implemented by errors whose own message differs from Error() (which may include the cause).
type Messenger interface{ Message() string }

// Throwable renders err and its causes, one per line: the error, then "Caused by: <cause>".
func Throwable(err error) string {
	var b strings.Builder
	first := true
	for e := err; e != nil; e = errors.Unwrap(e) {
		if !first {
			b.WriteString("Caused by: ")
		}
		first = false
		if m, ok := e.(Messenger); ok {
			b.WriteString(m.Message())
		} else {
			b.WriteString(e.Error())
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func slf4j(pattern string, args ...any) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '{' && i+1 < len(pattern) && pattern[i+1] == '}' && n < len(args) {
			b.WriteString(fmt.Sprint(args[n]))
			n++
			i++
			continue
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}
