//go:build unix

package sshconfig

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fixture = "../../testdata/ssh/config"

// TestMain gives the tests an empty home, so no test reads the real ~/.ssh.
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "home")
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func aliases(c *Config) []string {
	var a []string
	for _, h := range c.Hosts {
		a = append(a, h.Alias)
	}
	return a
}

func TestLoadFixture(t *testing.T) {
	c := Load(fixture)
	want := []string{"github", "home-server", "raspberry-pi", "production-api", "prod", "staging-api"}
	got := aliases(c)
	if len(got) != len(want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("aliases = %v, want %v", got, want)
		}
	}
	if len(c.Rules) != 3 {
		t.Errorf("rules = %d, want 3 (*.internal, Match, *)", len(c.Rules))
	}
	if !c.MatchExec {
		t.Error("MatchExec should be true")
	}
	if len(c.Problems) != 1 {
		t.Errorf("problems = %v, want 1 missing include", c.Problems)
	}
}

func TestMultiAliasShareBlock(t *testing.T) {
	c := Load(fixture)
	var a, b *Block
	for _, h := range c.Hosts {
		switch h.Alias {
		case "production-api":
			a = h.Block
		case "prod":
			b = h.Block
		}
	}
	if a == nil || a != b {
		t.Fatal("aliases of one Host line must share one block")
	}
	if a.Get("hostname") != "192.168.1.50" || a.Get("User") != "ubuntu" {
		t.Errorf("options wrong: %+v", a.Options)
	}
	if a.Get("CustomUnknownDirective") != "keep-me" {
		t.Errorf("unknown directive lost or comment not stripped: %q", a.Get("CustomUnknownDirective"))
	}
	if a.Included || a.Line != 5 {
		t.Errorf("block source wrong: included=%v line=%d", a.Included, a.Line)
	}
}

func TestIncludedFlagAndEqualsSyntax(t *testing.T) {
	c := Load(fixture)
	for _, h := range c.Hosts {
		switch h.Alias {
		case "github":
			if !h.Block.Included {
				t.Error("github should be included")
			}
		case "staging-api":
			if h.Block.Get("HostName") != "10.0.0.20" {
				t.Errorf("HostName= syntax: %q", h.Block.Get("HostName"))
			}
		}
	}
}

func TestMissingMain(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "nope"))
	if !c.Missing || len(c.Hosts) != 0 {
		t.Errorf("missing main not reported: %+v", c)
	}
}

func TestIncludeLoopAndDepth(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "config"), []byte("Include a\nHost x\n"), 0o600)
	os.WriteFile(filepath.Join(d, "a"), []byte("Include config\nHost y\n"), 0o600)
	c := Load(filepath.Join(d, "config"))
	if len(c.Problems) == 0 {
		t.Error("loop not reported")
	}
	if len(c.Hosts) != 2 {
		t.Errorf("hosts = %v", aliases(c))
	}
}

func TestSafeAlias(t *testing.T) {
	for _, bad := range []string{"", "-oProxyCommand=x", "a b", "a\nb"} {
		if SafeAlias(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if SafeAlias("web-1.example") != nil {
		t.Error("good alias rejected")
	}
}

func TestFuzzy(t *testing.T) {
	if FuzzyScore("pa", "production-api") == 0 || FuzzyScore("zz", "production-api") != 0 {
		t.Error("fuzzy basics wrong")
	}
	if FuzzyScore("prod", "production-api") <= FuzzyScore("prod", "staging-prod-x") {
		t.Error("prefix should score higher")
	}
}

func loadText(t *testing.T, text string) *Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte(text), 0o600)
	return Load(f)
}

func TestMatchExecForms(t *testing.T) {
	for _, text := range []string{
		"Match exec \"true\"\n",
		"match !exec \"true\"\n",
		"Match exec=\"true\"\n",
		"MATCH=EXEC true\n",
		"\"Match\" exec \"true\"\n",
		"Match\rexec \"true\"\n",
		"\tMatch\thost a\texec true\r\n",
	} {
		c := loadText(t, text)
		if !c.MatchExec || len(c.Rules) != 1 || c.Rules[0].Kind != "match" {
			t.Errorf("%q: MatchExec=%v rules=%d", text, c.MatchExec, len(c.Rules))
		}
	}
	if c := loadText(t, "Match host a\nHost b\n# Match exec x\n"); c.MatchExec || c.Untrusted {
		t.Error("plain config flagged")
	}
}

func TestUntrusted(t *testing.T) {
	long := "# " + strings.Repeat("x", maxFileBytes) + "\nMatch exec \"true\"\n"
	for name, text := range map[string]string{
		"oversized file":   long,
		"include variable": "Include ${HOME}/x\n",
		"include token":    "Include %d/x\n",
	} {
		c := loadText(t, text)
		if !c.Untrusted || len(c.Problems) == 0 {
			t.Errorf("%s: Untrusted=%v problems=%v", name, c.Untrusted, c.Problems)
		}
	}
}

func TestFifoIncludeDoesNotHang(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(d, "pipe"), 0o600); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(d, "config"), []byte("Include pipe\nHost x\n"), 0o600)
	done := make(chan *Config, 1)
	go func() { done <- Load(filepath.Join(d, "config")) }()
	select {
	case c := <-done:
		if len(c.Problems) != 1 || !c.Untrusted || len(c.Hosts) != 1 {
			t.Errorf("problems=%v untrusted=%v hosts=%v", c.Problems, c.Untrusted, aliases(c))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load hangs on a FIFO")
	}
}

func TestIncludeFanOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := t.TempDir()
	for i := 0; i < maxIncludeDepth; i++ {
		text := fmt.Sprintf("Host h%d\n", i) + strings.Repeat(fmt.Sprintf("Include f%d\n", i+1), 10)
		os.WriteFile(filepath.Join(d, fmt.Sprintf("f%d", i)), []byte(text), 0o600)
	}
	os.WriteFile(filepath.Join(d, fmt.Sprintf("f%d", maxIncludeDepth)), []byte("Host last\n"), 0o600)
	start := time.Now()
	c := Load(filepath.Join(d, "f0"))
	if time.Since(start) > 2*time.Second {
		t.Errorf("fan-out took %s", time.Since(start))
	}
	if len(c.Hosts) != maxIncludeDepth+1 || len(c.Problems) != 0 {
		t.Errorf("hosts=%d problems=%v", len(c.Hosts), c.Problems)
	}
}

func TestLoadBudgets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := t.TempDir()
	for i := 0; i < maxFiles+5; i++ {
		os.WriteFile(filepath.Join(d, fmt.Sprintf("inc%04d", i)), []byte("Host a\n"), 0o600)
	}
	os.WriteFile(filepath.Join(d, "config"), []byte("Include inc*\n"), 0o600)
	c := Load(filepath.Join(d, "config"))
	// Two problems: the pattern has more matches than its cap, and the file limit.
	if len(c.Files) != maxFiles || !c.Untrusted || len(c.Problems) != 2 {
		t.Errorf("files=%d untrusted=%v problems=%v", len(c.Files), c.Untrusted, c.Problems)
	}

	c = loadText(t, "Host big\n"+strings.Repeat("  User x\n", maxRawLines+50))
	if len(c.Hosts[0].Block.Raw) != maxRawLines || len(c.Problems) != 1 {
		t.Errorf("raw=%d problems=%v", len(c.Hosts[0].Block.Raw), c.Problems)
	}

	for _, text := range []string{
		strings.Repeat("Host a\n", maxBlocks+50),
		"Host" + strings.Repeat(" a", maxBlocks+50) + "\n", // one line, many aliases
	} {
		c = loadText(t, text)
		if len(c.Hosts) != maxBlocks || len(c.Problems) != 1 {
			t.Errorf("hosts=%d problems=%v", len(c.Hosts), c.Problems)
		}
	}
}

func TestIncludeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	dot := t.TempDir() // the real file lives outside ~/.ssh
	os.Mkdir(sshDir, 0o700)
	os.WriteFile(filepath.Join(dot, "config"), []byte("Include extra\n"), 0o600)
	os.WriteFile(filepath.Join(dot, "extra"), []byte("Host from-file-dir\n"), 0o600)
	os.WriteFile(filepath.Join(sshDir, "extra"), []byte("Host from-ssh-dir\n"), 0o600)

	// Not the user's main config: relative to the file.
	if got := aliases(Load(filepath.Join(dot, "config"))); len(got) != 1 || got[0] != "from-file-dir" {
		t.Errorf("other file: %v", got)
	}
	// The user's main config through a symlink: relative to ~/.ssh, by either name.
	os.Symlink(filepath.Join(dot, "config"), filepath.Join(sshDir, "config"))
	for _, p := range []string{filepath.Join(sshDir, "config"), filepath.Join(dot, "config")} {
		if got := aliases(Load(p)); len(got) != 1 || got[0] != "from-ssh-dir" {
			t.Errorf("%s: %v", p, got)
		}
	}
}

func TestEffectiveTimeoutKillsChildren(t *testing.T) {
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "#!/bin/sh\nsleep 30 &\necho $! > " + pidFile + "\nsleep 30\n"
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := effTimeout
	effTimeout = 1500 * time.Millisecond
	defer func() { effTimeout = old }()

	start := time.Now()
	_, err := Effective("/dev/null", "host")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v", err)
	}
	// Without the group kill the child keeps the pipes open until WaitDelay.
	if d := time.Since(start); d > 2800*time.Millisecond {
		t.Errorf("returned after %s", d)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	for i := 0; pid > 0 && syscall.Kill(pid, 0) == nil; i++ {
		if i == 20 {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("the child of ssh is still alive")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const groupFixture = "../../testdata/ssh-groups/config"

func hostBlock(t *testing.T, c *Config, alias string) *Block {
	t.Helper()
	for _, h := range c.Hosts {
		if h.Alias == alias {
			return h.Block
		}
	}
	t.Fatalf("no host %s in %v", alias, aliases(c))
	return nil
}

func TestGroupFixture(t *testing.T) {
	c := Load(groupFixture)
	if got := strings.Join(c.Groups(), ","); got != "Git,Work,Home" {
		t.Errorf("groups = %s, want first-seen file order", got)
	}
	for alias, want := range map[string]string{
		"github": "Git", "production-api": "Work", "prod": "Work", "staging-api": "Work",
		"raspberry-pi": "Home", "scratch": "", "not-directly-above": "",
	} {
		if got := hostBlock(t, c, alias).Group; got != want {
			t.Errorf("%s: group %q, want %q", alias, got, want)
		}
	}
	if b := hostBlock(t, c, "github"); !b.Included || b.GroupLine != 1 || b.Line != 2 {
		t.Errorf("included tag: included=%v groupLine=%d line=%d", b.Included, b.GroupLine, b.Line)
	}
	if a, b := hostBlock(t, c, "production-api"), hostBlock(t, c, "prod"); a != b || a.GroupLine != 4 || a.Line != 5 {
		t.Errorf("multi-alias block: same=%v groupLine=%d line=%d", a == b, a.GroupLine, a.Line)
	}
	for _, r := range c.Rules {
		if r.Group != "" || r.GroupLine != 0 {
			t.Errorf("rule %v has group %q", r.Patterns, r.Group)
		}
	}
	if !c.Hashed {
		t.Error("the main file has no hash")
	}
}

func TestGroupTagForms(t *testing.T) {
	long := strings.Repeat("x", MaxGroupName)
	for text, want := range map[string]string{
		"# sshwave: group=Work\nHost a\n":                   "Work",
		"# sshwave: group=  My Servers \t\nHost a\n":        "My Servers",
		"  \t# sshwave: group=Work\n  \tHost a\n":           "Work", // indented block
		"# sshwave: group=Work\r\nHost a\r\n":               "Work", // CRLF
		"# sshwave: group=" + long + "\nHost a\n":           long,
		"# sshwave: group=Work\nHost=a\n":                   "Work",
		"Host z\n  User u\n# sshwave: group=Work\nHost a\n": "Work",

		"# sshwave: group=Work\n\nHost a\n":             "", // blank line between
		"# sshwave: group=Work\n# note\nHost a\n":       "", // comment between
		"# sshwave: group=\nHost a\n":                   "", // empty name
		"# sshwave: group=   \nHost a\n":                "",
		"# sshwave: group=" + long + "x\nHost a\n":      "", // too long
		"# sshwave: group=a\x1b[2Jb\nHost a\n":          "", // control character
		"# sshwave: group=a\rb\nHost a\n":               "", // CR inside the name
		"# sshwave: group=\xff\nHost a\n":               "", // not UTF-8
		"# sshwave: Group=Work\nHost a\n":               "", // key is case-sensitive
		"#sshwave: group=Work\nHost a\n":                "",
		"#  sshwave: group=Work\nHost a\n":              "",
		"Host a\n# sshwave: group=Work\n":               "", // tag below
		"User x # sshwave: group=Work\nHost a\n":        "", // not a comment line
		"# sshwave: group=Work\nMatch host a\nHost a\n": "",
	} {
		c := loadText(t, text)
		b := hostBlock(t, c, "a")
		if b.Group != want {
			t.Errorf("%q: group %q, want %q", text, b.Group, want)
		}
		if (b.GroupLine != 0) != (want != "") || (want != "" && b.GroupLine != b.Line-1) {
			t.Errorf("%q: groupLine %d, host line %d", text, b.GroupLine, b.Line)
		}
		if len(c.Problems) != 0 {
			t.Errorf("%q: problems %v", text, c.Problems)
		}
	}
}

func TestTagsAreNotInTheRawOfTheBlockAbove(t *testing.T) {
	c := loadText(t, "Host z\n  User u\n\n# sshwave: group=Work\n# sshwave: note=n\nHost a\n  User v\n# sshwave: note=last\nHost b\n")
	for alias, want := range map[string]string{"z": "Host z|  User u", "a": "Host a|  User v", "b": "Host b"} {
		if got := strings.Join(hostBlock(t, c, alias).Raw, "|"); got != want {
			t.Errorf("%s: raw = %q, want %q", alias, got, want)
		}
	}
	if z, a := hostBlock(t, c, "z"), hostBlock(t, c, "a"); z.Group != "" || z.Note != nil || a.Group != "Work" {
		t.Error("the tags belong to the block below them")
	}
	// A tag that is in no stack is a comment of the block above it.
	c = loadText(t, "Host z\n# sshwave: note=far\n\nHost a\n# sshwave: group=G\nMatch all\n# sshwave: note=end\n")
	if got := strings.Join(hostBlock(t, c, "z").Raw, "|"); got != "Host z|# sshwave: note=far" {
		t.Errorf("z: raw = %q", got)
	}
	if got := strings.Join(hostBlock(t, c, "a").Raw, "|"); got != "Host a|# sshwave: group=G" {
		t.Errorf("a: raw = %q", got)
	}
	if got := strings.Join(c.Rules[0].Raw, "|"); got != "Match all|# sshwave: note=end" {
		t.Errorf("match: raw = %q", got)
	}
	// The stack of the next block does not count for the cap on raw lines,
	// and the cap cannot hide it.
	c = loadText(t, "Host z\n"+strings.Repeat("  User u\n", maxRawLines-1)+"# sshwave: group=Work\n# sshwave: note=n\nHost a\n")
	if z := hostBlock(t, c, "z"); len(z.Raw) != maxRawLines || len(c.Problems) != 0 {
		t.Errorf("raw=%d problems=%v", len(z.Raw), c.Problems)
	}
	c = loadText(t, "Host z\n"+strings.Repeat("  User u\n", maxRawLines+5)+"# sshwave: group=Work\n# sshwave: note=n\nHost a\n")
	if a := hostBlock(t, c, "a"); a.Group != "Work" || strings.Join(a.Note, "|") != "n" || len(c.Problems) != 1 {
		t.Errorf("tags lost after a long block: %+v problems=%v", a, c.Problems)
	}
}

func TestGroupsOnlyFromHosts(t *testing.T) {
	c := loadText(t, "# sshwave: group=Rules\nHost *.x\n# sshwave: group=B\nHost b *.y\n# sshwave: group=A\nHost a\n# sshwave: group=B\nHost c\n")
	if got := strings.Join(c.Groups(), ","); got != "B,A" {
		t.Errorf("groups = %q", got)
	}
	if len(c.Rules) != 2 || c.Rules[0].Group != "" || c.Rules[1].Group != "B" {
		t.Errorf("a block with only patterns gets no group: %+v", c.Rules)
	}
}

func TestHashIsForTheMainFileOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := t.TempDir()
	main := "Include inc\nHost a\n"
	os.WriteFile(filepath.Join(d, "config"), []byte(main), 0o600)
	os.WriteFile(filepath.Join(d, "inc"), []byte("Host b\n"), 0o600)
	c := Load(filepath.Join(d, "config"))
	if !c.Hashed || c.Hash != sha256.Sum256([]byte(main)) {
		t.Error("hash is not that of the main file")
	}
	c = loadText(t, "# "+strings.Repeat("x", maxFileBytes)+"\nHost a\n")
	if c.Hashed {
		t.Error("a truncated file must have no hash")
	}
}

func TestHostPatterns(t *testing.T) {
	for line, want := range map[string]string{
		"Host a b":       "a,b",
		"  host=a":       "a",
		"\"HOST\" a\r":   "a",
		"Host \"a b\" c": "a b,c",
	} {
		if got, ok := HostPatterns(line); !ok || strings.Join(got, ",") != want {
			t.Errorf("%q: %v %v", line, got, ok)
		}
	}
	for _, line := range []string{"", "# Host a", "HostName a", "Match host a"} {
		if _, ok := HostPatterns(line); ok {
			t.Errorf("%q is not a Host line", line)
		}
	}
}
