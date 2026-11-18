// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/Samrat7745/cli/go/internal/jc"
)

// PathTypeConverterTest: anything containing the NUL character is not a path.
func TestPathTypeConverter_givenInvalidPath_whenConverting_thenThrows(t *testing.T) {
	_, err := parsePathArg(random(10) + "\x00")
	if _, ok := err.(*jc.ParameterError); !ok {
		t.Fatalf("expected ParameterException, got %v", err)
	}
	if p, err := parsePathArg("/some/path"); err != nil || p != "/some/path" {
		t.Fatal(p, err)
	}
}
