//go:build unix

package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadWithin loads path and fails when the load takes longer than limit.
func loadWithin(t *testing.T, path string, limit time.Duration) *Config {
	t.Helper()
	done := make(chan *Config, 1)
	start := time.Now()
	go func() { done <- Load(path) }()
	select {
	case c := <-done:
		t.Logf("load took %s", time.Since(start))
		return c
	case <-time.After(limit):
		t.Fatalf("Load took more than %s", limit)
		return nil
	}
}

func manyFiles(t *testing.T, n int) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "d"), 0o700)
	for i := 0; i < n; i++ {
		os.WriteFile(filepath.Join(d, "d", fmt.Sprintf("f%04d", i)), []byte("Host a\n"), 0o600)
	}
	return d
}

// A small config must not cause much work: one line with 2000 globs over
// 2000 files took 40 s before the caps.
func TestIncludeLineWorkIsBounded(t *testing.T) {
	d := manyFiles(t, 2000)
	f := filepath.Join(d, "config")
	os.WriteFile(f, []byte("Include"+strings.Repeat(" d/*", 2000)+"\n"), 0o600)
	c := loadWithin(t, f, time.Second)
	if len(c.Files) != maxFiles || !c.Untrusted || len(c.Problems) == 0 {
		t.Errorf("files=%d untrusted=%v problems=%v", len(c.Files), c.Untrusted, c.Problems)
	}
}

// Many Include lines must not cause much work either.
func TestIncludeLinesWorkIsBounded(t *testing.T) {
	d := manyFiles(t, 200)
	f := filepath.Join(d, "config")
	line := "Include d/*\n"
	os.WriteFile(f, []byte(strings.Repeat(line, maxFileBytes/len(line))), 0o600)
	c := loadWithin(t, f, time.Second)
	if len(c.Files) != 201 || !c.Untrusted || len(c.Problems) == 0 {
		t.Errorf("files=%d untrusted=%v problems=%v", len(c.Files), c.Untrusted, c.Problems)
	}
}

func TestIncludeCaps(t *testing.T) {
	// More tokens on one line than the cap: the rest is not read.
	d := manyFiles(t, 0)
	text := "Include"
	for i := 0; i < maxIncludeTokens+5; i++ {
		os.WriteFile(filepath.Join(d, fmt.Sprintf("t%03d", i)), []byte(fmt.Sprintf("Host t%d\n", i)), 0o600)
		text += fmt.Sprintf(" t%03d", i)
	}
	f := filepath.Join(d, "config")
	os.WriteFile(f, []byte(text+"\n"), 0o600)
	c := Load(f)
	if len(c.Hosts) != maxIncludeTokens || !c.Untrusted || len(c.Problems) != 1 {
		t.Errorf("token cap: hosts=%d untrusted=%v problems=%v", len(c.Hosts), c.Untrusted, c.Problems)
	}

	// More glob matches for one pattern than the cap: the rest is not read.
	// Folders match too, and they use no file budget.
	d = manyFiles(t, 0)
	for i := 0; i < maxGlobMatches; i++ {
		os.Mkdir(filepath.Join(d, "d", fmt.Sprintf("a%04d", i)), 0o700)
	}
	os.WriteFile(filepath.Join(d, "d", "z"), []byte("Host z\n"), 0o600)
	f = filepath.Join(d, "config")
	os.WriteFile(f, []byte("Include d/*\n"), 0o600)
	c = Load(f)
	if len(c.Hosts) != 0 || !c.Untrusted || len(c.Problems) != 1 {
		t.Errorf("glob cap: hosts=%d untrusted=%v problems=%v", len(c.Hosts), c.Untrusted, c.Problems)
	}
}

func TestProblemsAreCapped(t *testing.T) {
	for name, text := range map[string]string{
		"one long line": "Include" + strings.Repeat(" $", maxFileBytes/2-8) + "\n",
		"many lines":    strings.Repeat("Include $\n", maxFileBytes/10),
	} {
		c := loadWithin(t, func() string {
			t.Setenv("HOME", t.TempDir())
			f := filepath.Join(t.TempDir(), "config")
			os.WriteFile(f, []byte(text), 0o600)
			return f
		}(), time.Second)
		if n := len(c.Problems); n == 0 || n > MaxProblems+1 || !c.Untrusted {
			t.Errorf("%s: problems=%d untrusted=%v", name, n, c.Untrusted)
		}
		if name == "many lines" {
			if last := c.Problems[len(c.Problems)-1]; len(c.Problems) != MaxProblems+1 || !strings.Contains(last.Msg, "more problems") {
				t.Errorf("%s: %d problems, last %q", name, len(c.Problems), last.Msg)
			}
		}
	}
}

// The warning must be stronger when ssh can read other files than the app.
func TestUntrustedIncludes(t *testing.T) {
	for name, text := range map[string]string{
		"other user":  "Include ~nobody/conf\n",
		"bad pattern": "Include /tmp/[x\n",
	} {
		if c := loadText(t, text); !c.Untrusted || len(c.Problems) != 1 || strings.Contains(c.Problems[0].Msg, "missing") {
			t.Errorf("%s: untrusted=%v problems=%v", name, c.Untrusted, c.Problems)
		}
	}

	// A relative Include: ssh resolves it against ~/.ssh, the app against
	// the folder of a file that is not ~/.ssh/config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	for _, text := range []string{"Include extra\n", "Include sub/*\n"} {
		other := filepath.Join(t.TempDir(), "config")
		os.WriteFile(other, []byte(text), 0o600)
		if c := Load(other); !c.Untrusted {
			t.Errorf("other path, %q: not Untrusted", text)
		}
		def := filepath.Join(home, ".ssh", "config")
		os.WriteFile(def, []byte(text), 0o600)
		if c := Load(def); c.Untrusted {
			t.Errorf("default path, %q: Untrusted", text)
		}
	}
	other := filepath.Join(t.TempDir(), "config")
	os.WriteFile(other, []byte("Include ~/x/*\nInclude /nonexistent/x/*\nHost a\n"), 0o600)
	if c := Load(other); c.Untrusted {
		t.Error("other path with absolute Includes: Untrusted")
	}
}
