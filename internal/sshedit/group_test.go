package sshedit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansku/sshwave/internal/backup"
	"github.com/dansku/sshwave/internal/sshconfig"
)

// TestMain gives the tests an empty home and data folder, so no test reads
// the real ~/.ssh or writes a backup outside a temporary folder.
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "home")
	os.Setenv("HOME", home)
	os.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// write makes a config file in a new temporary folder.
func write(t *testing.T, text string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func read(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// names returns the entries of the folder of f.
func names(t *testing.T, f string) string {
	t.Helper()
	es, err := os.ReadDir(filepath.Dir(f))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return strings.Join(out, ",")
}

// set loads f as the app does and moves alias to group.
func set(t *testing.T, f, alias, group string) (string, error) {
	t.Helper()
	c := sshconfig.Load(f)
	for _, h := range c.Hosts {
		if h.Alias == alias {
			return SetGroup(f, c.Hash, h.Block.Line, h.Block.Patterns, group)
		}
	}
	t.Fatalf("no host %s", alias)
	return "", nil
}

func TestSetGroupEdits(t *testing.T) {
	for _, tc := range []struct{ name, before, group, after string }{
		{"insert", "Host z\n\nHost a b\n  User u\n", "Work", "Host z\n\n# sshwave: group=Work\nHost a b\n  User u\n"},
		{"insert first line", "Host a\n", "Work", "# sshwave: group=Work\nHost a\n"},
		{"insert CRLF", "Host z\r\nHost a\r\n  User u\r\n", "Work", "Host z\r\n# sshwave: group=Work\r\nHost a\r\n  User u\r\n"},
		{"insert in a mixed file", "Host z\r\nHost a\n", "Work", "Host z\r\n# sshwave: group=Work\nHost a\n"},
		{"insert keeps the indent", "Host z\n \t Host a\n", "Work", "Host z\n \t # sshwave: group=Work\n \t Host a\n"},
		{"insert no final newline", "Host z\nHost a", "Work", "Host z\n# sshwave: group=Work\nHost a"},
		{"insert no final newline CRLF", "Host z\r\nHost a", "Work", "Host z\r\n# sshwave: group=Work\r\nHost a"},
		{"insert one line no newline", "Host a", "Work", "# sshwave: group=Work\nHost a"},
		{"insert below a comment", "# note\nHost a\n", "Work", "# note\n# sshwave: group=Work\nHost a\n"},
		{"insert below a far tag", "# sshwave: group=Old\n\nHost a\n", "Work", "# sshwave: group=Old\n\n# sshwave: group=Work\nHost a\n"},
		{"insert with odd bytes", "Host z\t \n\x00\xff\n\n\nHost=a  # c\n\tUser  u\n\n", "My Servers", "Host z\t \n\x00\xff\n\n\n# sshwave: group=My Servers\nHost=a  # c\n\tUser  u\n\n"},
		{"replace", "Host z\n# sshwave: group=Old\nHost a\n  User u\n", "Work", "Host z\n# sshwave: group=Work\nHost a\n  User u\n"},
		{"replace CRLF", "# sshwave: group=Old\r\nHost a\r\n", "Work", "# sshwave: group=Work\r\nHost a\r\n"},
		{"replace keeps the tag indent", "\t# sshwave: group=Old  \nHost a\n", "Work", "\t# sshwave: group=Work\nHost a\n"},
		{"replace no final newline", "# sshwave: group=Old\nHost a", "Work", "# sshwave: group=Work\nHost a"},
		{"replace a bad tag", "# sshwave: group=\nHost a\n", "Work", "# sshwave: group=Work\nHost a\n"},
		{"clear", "Host z\n# sshwave: group=Old\nHost a\n  User u\n", "", "Host z\nHost a\n  User u\n"},
		{"clear CRLF", "Host z\r\n# sshwave: group=Old\r\nHost a\r\n", "", "Host z\r\nHost a\r\n"},
		{"clear first line", "# sshwave: group=Old\nHost a", "", "Host a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := write(t, tc.before)
			bak, err := set(t, f, "a", tc.group)
			if err != nil {
				t.Fatal(err)
			}
			if got := read(t, f); got != tc.after {
				t.Errorf("file\n got %q\nwant %q", got, tc.after)
			}
			if got := read(t, bak); got != tc.before {
				t.Errorf("backup %q, want the exact bytes of before", got)
			}
			if got := names(t, f); got != "config" {
				t.Errorf("folder has %s", got)
			}
			// The parser reads back what the writer wrote, and the next
			// change of the same block replaces the tag.
			c := sshconfig.Load(f)
			for _, h := range c.Hosts {
				if h.Alias == "a" && h.Block.Group != tc.group {
					t.Errorf("parsed group %q, want %q", h.Block.Group, tc.group)
				}
			}
			if _, err := set(t, f, "a", "Next"); err != nil {
				t.Fatal(err)
			}
			if _, err := set(t, f, "a", tc.group); err != nil {
				t.Fatal(err)
			}
			if got := read(t, f); got != tc.after {
				t.Errorf("after two more changes\n got %q\nwant %q", got, tc.after)
			}
		})
	}
}

func TestSetGroupChangesOnlyOneLine(t *testing.T) {
	src, err := os.ReadFile("../../testdata/ssh-groups/config")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		alias, group string
		delta        int // lines added
	}{{"scratch", "Work", 1}, {"staging-api", "Home", 0}, {"raspberry-pi", "", -1}} {
		f := write(t, string(src))
		c := sshconfig.Load(f)
		var line int
		for _, h := range c.Hosts {
			if h.Alias == tc.alias {
				line = h.Block.Line
			}
		}
		if _, err := set(t, f, tc.alias, tc.group); err != nil {
			t.Fatal(err)
		}
		before := bytes.SplitAfter(src, []byte("\n"))
		after := bytes.SplitAfter([]byte(read(t, f)), []byte("\n"))
		if len(after) != len(before)+tc.delta {
			t.Fatalf("%s: %d lines, want %d", tc.alias, len(after), len(before)+tc.delta)
		}
		// The edited line is the one above the Host line. For an insert the
		// Host line had no tag, so the lines above it are all unchanged.
		head := line - 2 // 0-based index of the tag line in before
		if tc.delta == 1 {
			head = line - 1
		}
		if !bytes.Equal(bytes.Join(before[:head], nil), bytes.Join(after[:head], nil)) {
			t.Errorf("%s: bytes above the edit changed", tc.alias)
		}
		if !bytes.Equal(bytes.Join(before[line-1:], nil), bytes.Join(after[line-1+tc.delta:], nil)) {
			t.Errorf("%s: bytes from the Host line on changed", tc.alias)
		}
	}
}

func TestSetGroupSameGroupWritesNothing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, tc := range [][2]string{{"# sshwave: group=Work\nHost a\n", "Work"}, {"Host a\n", ""}} {
		f := write(t, tc[0])
		st, _ := os.Stat(f)
		bak, err := set(t, f, "a", tc[1])
		if err != nil || bak != "" {
			t.Errorf("bak=%q err=%v", bak, err)
		}
		if now, _ := os.Stat(f); !os.SameFile(st, now) || read(t, f) != tc[0] {
			t.Error("the file was replaced")
		}
	}
	if es, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "sshwave", "backups")); len(es) != 0 {
		t.Errorf("%d backups for no change", len(es))
	}
}

func TestSetGroupRefusesBadNames(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const text = "Host a\n  User u\n"
	f := write(t, text)
	c := sshconfig.Load(f)
	b := c.Hosts[0].Block
	for _, name := range []string{
		"x\nProxyCommand evil", "x\rProxyCommand evil", "x\r\nHost *", "\nx", "x\n",
		"a\x00b", "a\x1bb", "a\tb", "a\u0085b", " x", "x ", strings.Repeat("x", 65), "\xff",
	} {
		if _, err := SetGroup(f, c.Hash, b.Line, b.Patterns, name); err == nil {
			t.Errorf("%q accepted", name)
		}
		if got := read(t, f); got != text {
			t.Fatalf("%q changed the file: %q", name, got)
		}
	}
	if es, _ := os.ReadDir(os.Getenv("XDG_DATA_HOME")); len(es) != 0 {
		t.Error("a refused name touched the backup folder")
	}
	if _, err := SetGroup(f, c.Hash, b.Line, b.Patterns, strings.Repeat("x", 64)); err != nil {
		t.Errorf("64 bytes refused: %v", err)
	}
}

func TestSetGroupStaleFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := write(t, "Host a\n")
	c := sshconfig.Load(f)
	b := c.Hosts[0].Block
	const changed = "Host a\n  User u\n" // the Host line is still the same
	os.WriteFile(f, []byte(changed), 0o600)
	if _, err := SetGroup(f, c.Hash, b.Line, b.Patterns, "Work"); !errors.Is(err, ErrStale) {
		t.Errorf("err = %v", err)
	}
	if read(t, f) != changed {
		t.Error("a stale file was written")
	}
	if es, _ := os.ReadDir(os.Getenv("XDG_DATA_HOME")); len(es) != 0 {
		t.Error("a stale file got a backup")
	}
}

func TestSetGroupHostLineMismatch(t *testing.T) {
	const text = "# c\nHost a b\n  User u\nHost c\n"
	f := write(t, text)
	hash := sha256.Sum256([]byte(text))
	for _, tc := range []struct {
		line     int
		patterns []string
	}{
		{0, []string{"a", "b"}}, {1, []string{"a", "b"}}, {3, []string{"a", "b"}}, {5, []string{"a", "b"}}, {99, []string{"a", "b"}},
		{2, []string{"a"}}, {2, []string{"b", "a"}}, {2, []string{"a", "b", "c"}}, {2, nil}, {4, []string{"a", "b"}},
	} {
		if _, err := SetGroup(f, hash, tc.line, tc.patterns, "Work"); err == nil {
			t.Errorf("line %d %v accepted", tc.line, tc.patterns)
		}
		if read(t, f) != text {
			t.Fatalf("line %d %v changed the file", tc.line, tc.patterns)
		}
	}
	if _, err := SetGroup(f, hash, 2, []string{"a", "b"}, "Work"); err != nil {
		t.Error(err)
	}
}

func TestSetGroupKeepsTheMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644, 0o400, 0o640} {
		f := write(t, "Host a\n")
		os.Chmod(f, mode)
		if _, err := set(t, f, "a", "Work"); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(f); st.Mode().Perm() != mode {
			t.Errorf("mode %v, want %v", st.Mode().Perm(), mode)
		}
	}
}

func TestSetGroupThroughSymlink(t *testing.T) {
	d := t.TempDir()
	target := write(t, "Host a\n") // in another folder
	link := filepath.Join(d, "config")
	os.Symlink(target, link)
	if _, err := set(t, link, "a", "Work"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
	if got := read(t, target); got != "# sshwave: group=Work\nHost a\n" {
		t.Errorf("target = %q", got)
	}
	if got := names(t, link); got != "config" {
		t.Errorf("the folder of the symlink has %s", got)
	}
	if got := names(t, target); got != "config" {
		t.Errorf("the folder of the target has %s", got)
	}
}

func TestSetGroupReadOnlyFolder(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	const text = "Host a\n"
	f := write(t, text)
	dir := filepath.Dir(f)
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := set(t, f, "a", "Work"); err == nil {
		t.Fatal("want an error")
	}
	if read(t, f) != text || names(t, f) != "config" {
		t.Errorf("file %q, folder %s", read(t, f), names(t, f))
	}
	if es, _ := os.ReadDir(filepath.Join(xdg, "sshwave", "backups")); len(es) != 0 {
		t.Error("the backup of a failed write must not stay")
	}
}

func TestReplaceFileRemovesTheTempFile(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "config") // a folder with content: rename fails
	os.Mkdir(target, 0o700)
	os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600)
	if err := replaceFile(target, []byte("new"), 0o600); err == nil {
		t.Fatal("want an error")
	}
	if es, _ := os.ReadDir(d); len(es) != 1 {
		t.Errorf("%d entries, the temporary file must be gone", len(es))
	}
	if read(t, filepath.Join(target, "keep")) != "x" {
		t.Error("the target changed")
	}
}

func TestSetGroupBackupFailureWritesNothing(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	xdg := t.TempDir()
	os.Chmod(xdg, 0o500)
	t.Cleanup(func() { os.Chmod(xdg, 0o700) })
	t.Setenv("XDG_DATA_HOME", xdg)
	const text = "Host a\n"
	f := write(t, text)
	st, _ := os.Stat(f)
	_, err := set(t, f, "a", "Work")
	if err == nil || !strings.Contains(err.Error(), "backup failed") {
		t.Fatalf("err = %v", err)
	}
	if now, _ := os.Stat(f); !os.SameFile(st, now) || read(t, f) != text || names(t, f) != "config" {
		t.Error("the file changed without a backup")
	}
}

func TestSetGroupRefusesOtherFiles(t *testing.T) {
	d := t.TempDir()
	var zero [sha256.Size]byte
	if _, err := SetGroup(filepath.Join(d, "none"), zero, 1, []string{"a"}, "Work"); err == nil {
		t.Error("a missing file was accepted")
	}
	if _, err := SetGroup(d, zero, 1, []string{"a"}, "Work"); err == nil {
		t.Error("a folder was accepted")
	}
}

// A write that fails must not cost an old backup, and a write that works
// keeps the newest backup.Keep.
func TestPruneOnlyAfterTheWrite(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	f := write(t, "Host a\n")
	real, _ := filepath.EvalSymlinks(f)
	for i := 0; i < backup.Keep; i++ {
		if _, err := backup.CreateFromBytes(real, []byte("old\n")); err != nil {
			t.Fatal(err)
		}
	}
	bdir := filepath.Join(xdg, "sshwave", "backups", "x")
	full := names(t, bdir)

	dir := filepath.Dir(f)
	os.Chmod(dir, 0o500)
	if _, err := set(t, f, "a", "Work"); err == nil {
		t.Fatal("want an error")
	}
	os.Chmod(dir, 0o700)
	if got := names(t, bdir); got != full {
		t.Errorf("a failed write lost a backup:\n%s\nwant\n%s", got, full)
	}

	bak, err := set(t, f, "a", "Work")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(names(t, bdir), ",")
	if len(got) != backup.Keep || got[0] != strings.Split(full, ",")[1] || got[len(got)-1] != filepath.Base(bak) {
		t.Errorf("after a write: %d backups, first %s, last %s", len(got), got[0], got[len(got)-1])
	}
}
