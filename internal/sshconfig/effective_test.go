//go:build unix

package sshconfig

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeSSH puts a shell script with the name ssh first in PATH.
func fakeSSH(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\n"+script), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestEffectiveOutputIsCapped(t *testing.T) {
	for name, script := range map[string]string{
		"stdout": "while :; do echo 0123456789012345678901234567890123456789; done\n",
		"stderr": "while :; do echo 0123456789012345678901234567890123456789 >&2; done\n",
	} {
		fakeSSH(t, script)
		start := time.Now()
		pairs, err := Effective("/dev/null", "host")
		if err == nil || !strings.Contains(err.Error(), "more than 1 MiB") || pairs != nil {
			t.Errorf("%s: pairs=%d err=%v", name, len(pairs), err)
		}
		if d := time.Since(start); d > effTimeout/2 {
			t.Errorf("%s: returned after %s", name, d)
		}
	}

	// Output below the cap is read as before.
	fakeSSH(t, "echo 'hostname a.example'\necho 'port 22'\necho note >&2\n")
	pairs, err := Effective("/dev/null", "host")
	if err != nil || len(pairs) != 2 || pairs[0] != (Pair{"hostname", "a.example"}) {
		t.Errorf("pairs=%v err=%v", pairs, err)
	}
}

// ssh reads /etc/ssh/ssh_config only without -F. So the default path gets no
// -F, and each other path gets one.
func TestArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	def := filepath.Join(home, ".ssh", "config")
	os.WriteFile(def, []byte("Host a\n"), 0o600)
	other := filepath.Join(t.TempDir(), "config")
	os.WriteFile(other, []byte("Host a\n"), 0o600)
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(def, link)
	realDef, _ := filepath.EvalSymlinks(def)

	for _, tc := range []struct {
		name, path string
		effective  bool
		want       []string
	}{
		{"default, connect", def, false, []string{"--", "a"}},
		{"default, effective", def, true, []string{"-G", "--", "a"}},
		{"default, real path", realDef, true, []string{"-G", "--", "a"}},
		{"default, by symlink", link, true, []string{"-G", "--", "a"}},
		{"other, connect", other, false, []string{"-F", other, "--", "a"}},
		{"other, effective", other, true, []string{"-F", other, "-G", "--", "a"}},
		{"missing", filepath.Join(home, "nope"), true, []string{"-F", filepath.Join(home, "nope"), "-G", "--", "a"}},
	} {
		if got := Args(tc.path, tc.effective, "a"); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}

	// ~/.ssh/config as a symlink to another file: both names are the default.
	os.Remove(def)
	os.Symlink(other, def)
	for _, p := range []string{def, other} {
		if got := Args(p, false, "a"); !slices.Equal(got, []string{"--", "a"}) {
			t.Errorf("symlinked default %s: %q", p, got)
		}
	}
}
