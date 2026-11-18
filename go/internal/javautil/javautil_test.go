// Copyright (c) 2026 - for information on the respective copyright owner
// see the NOTICE file and/or the repository https://github.com/carbynestack/cli.
//
// SPDX-License-Identifier: Apache-2.0

package javautil

import "testing"

// UUID.fromString of Java 17, as used by UUIDTypeConverter.
func TestParseUUID(t *testing.T) {
	for in, want := range map[string]string{
		"3d908814-02a1-44f6-9e93-0d1d321eec3f": "3d908814-02a1-44f6-9e93-0d1d321eec3f",
		"3D908814-02A1-44F6-9E93-0D1D321EEC3F": "3d908814-02a1-44f6-9e93-0d1d321eec3f",
		"1-2-3-4-5":                            "00000001-0002-0003-0004-000000000005",
		"-1-2-3-4-5":                           "", // empty first field
	} {
		u, err := ParseUUID(in)
		if want == "" {
			if err == nil {
				t.Errorf("%s: expected an error", in)
			}
			continue
		}
		if err != nil || u.String() != want {
			t.Errorf("%s: %v %v", in, u, err)
		}
	}
	for _, bad := range []string{"", "bad-uuid", "!d908814-02a1-44f6-9e93-0d1d321eec3f",
		"3d908814-02a1-44f6-9e93-0d1d321eec3f:05c42ac1-7509-491e-93a4-11bc4bb28d64", "1-2-3-4-5-6", "1-2-3-4"} {
		if _, err := ParseUUID(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestParseNumbers(t *testing.T) {
	if v, err := ParseBigInteger("-42"); err != nil || v.String() != "-42" {
		t.Fatal(v, err)
	}
	for _, bad := range []string{"", "+", "4 2", "0x10", "--1"} {
		if _, err := ParseBigInteger(bad); err == nil {
			t.Errorf("BigInteger %q", bad)
		}
	}
	if n, err := ParseInt("+7"); err != nil || n != 7 {
		t.Fatal(n, err)
	}
	if _, err := ParseInt("2147483648"); err == nil {
		t.Fatal("int overflow")
	}
	if ListString([]int{1, 2}) != "[1, 2]" || ListString([]string{}) != "[]" {
		t.Fatal("ListString")
	}
}
