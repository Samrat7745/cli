// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

// Package javautil holds the Java library behaviour the CLI's argument handling depends on.
package javautil

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ParseUUID is java.util.UUID.fromString (Java 17): five dash separated hexadecimal fields, each
// masked to its width, so short and signed fields are accepted ("1-2-3-4-5").
func ParseUUID(s string) (uuid.UUID, error) {
	if len(s) > 36 {
		return uuid.Nil, errors.New("UUID string too large")
	}
	invalid := fmt.Errorf("Invalid UUID string: %s", s)
	var dash [5]int
	from := 0
	for k := 0; k < 5; k++ {
		i := strings.IndexByte(s[min(from, len(s)):], '-')
		if i < 0 {
			dash[k] = -1
		} else {
			dash[k] = from + i
		}
		if dash[k] >= 0 {
			from = dash[k] + 1
		} else {
			from = len(s) + 1
		}
	}
	if dash[3] < 0 || dash[4] >= 0 {
		return uuid.Nil, invalid
	}
	fields := []string{s[:dash[0]], s[dash[0]+1 : dash[1]], s[dash[1]+1 : dash[2]], s[dash[2]+1 : dash[3]], s[dash[3]+1:]}
	masks := []uint{32, 16, 16, 16, 48}
	var v [5]uint64
	for i, f := range fields {
		n, err := parseJavaLong(f, 16)
		if err != nil {
			return uuid.Nil, err
		}
		v[i] = uint64(n) & (1<<masks[i] - 1)
	}
	var u uuid.UUID
	msb := v[0]<<32 | v[1]<<16 | v[2]
	lsb := v[3]<<48 | v[4]
	for i := 0; i < 8; i++ {
		u[i] = byte(msb >> (56 - 8*i))
		u[8+i] = byte(lsb >> (56 - 8*i))
	}
	return u, nil
}

// parseJavaLong is Long.parseLong(s, radix): optional sign, at least one digit, range of a long.
func parseJavaLong(s string, radix int) (int64, error) {
	nfe := fmt.Errorf("For input string: \"%s\"", s)
	if s == "" {
		return 0, nfe
	}
	digits := s
	if s[0] == '+' || s[0] == '-' {
		digits = s[1:]
	}
	if digits == "" || strings.ContainsAny(digits, "+-") {
		return 0, nfe
	}
	n, err := strconv.ParseInt(s, radix, 64)
	if err != nil {
		return 0, nfe
	}
	return n, nil
}

// ParseInt is Integer.parseInt.
func ParseInt(s string) (int, error) {
	digits := strings.TrimLeft(s, "+-")
	if s == "" || len(s)-len(digits) > 1 || digits == "" {
		return 0, fmt.Errorf("For input string: \"%s\"", s)
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("For input string: \"%s\"", s)
	}
	return int(n), nil
}

// ParseLong is Long.parseLong.
func ParseLong(s string) (int64, error) { return parseJavaLong(s, 10) }

// ParseBigInteger is new BigInteger(String): an optional sign followed by decimal digits.
func ParseBigInteger(s string) (*big.Int, error) {
	if s == "" {
		return nil, errors.New("Zero length BigInteger")
	}
	digits := s
	if s[0] == '+' || s[0] == '-' {
		digits = s[1:]
	}
	if digits == "" || strings.ContainsAny(digits, "+-") {
		return nil, errors.New("Zero length BigInteger")
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("For input string: \"%s\"", s)
	}
	return v, nil
}

// ListString is AbstractCollection.toString: "[a, b]".
func ListString[T any](items []T) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = fmt.Sprint(it)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// RuntimeError is an unchecked Java exception that the Java CLI does not catch (for example a
// NumberFormatException of an argument): the JVM prints it to stderr and exits with status 1.
type RuntimeError struct {
	// Class is the Java exception class, e.g. "java.lang.IllegalArgumentException".
	Class string
	Msg   string
	Cause error
}

func (e *RuntimeError) Error() string {
	if e.Msg == "" {
		return e.Class
	}
	return e.Class + ": " + e.Msg
}

// Message is the exception message.
func (e *RuntimeError) Message() string { return e.Error() }

func (e *RuntimeError) Unwrap() error { return e.Cause }

// IllegalArgument returns a java.lang.IllegalArgumentException.
func IllegalArgument(msg string, cause error) error {
	return &RuntimeError{Class: "java.lang.IllegalArgumentException", Msg: msg, Cause: cause}
}

// NumberFormat returns a java.lang.NumberFormatException.
func NumberFormat(msg string) error {
	return &RuntimeError{Class: "java.lang.NumberFormatException", Msg: msg}
}
