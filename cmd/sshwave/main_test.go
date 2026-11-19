package main

import (
	"bytes"
	"testing"
)

// These tests stop before the app starts, so they read no config.

func TestVersionFlag(t *testing.T) {
	old := version
	version = "v1.2.3"
	defer func() { version = old }()
	var out, errb bytes.Buffer
	if code := run([]string{"-version"}, &out, &errb); code != 0 || out.String() != "sshwave v1.2.3\n" || errb.Len() != 0 {
		t.Errorf("code=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
}

func TestBadArguments(t *testing.T) {
	for _, args := range [][]string{{"-nope"}, {"stray"}, {"-config", "f", "stray"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 || errb.Len() == 0 {
			t.Errorf("%v: code=%d stderr=%q", args, code, errb.String())
		}
	}
}
