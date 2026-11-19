//go:build unix

package edit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/dansku/sshwave/internal/backup"
)

func TestFinishUnchangedRemovesBackup(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	s, err := Prepare(f)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.Finish()
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(s.Backup); !os.IsNotExist(err) {
		t.Error("backup should be removed")
	}
}

func TestFinishChangedKeepsBackup(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	s, _ := Prepare(f)
	os.WriteFile(f, []byte("Host b\n"), 0o600)
	changed, _ := s.Finish()
	if !changed {
		t.Fatal("want changed")
	}
	b, _ := os.ReadFile(s.Backup)
	if string(b) != "Host a\n" {
		t.Errorf("backup = %q", b)
	}
}

func TestSymlinkEditsTarget(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	d := t.TempDir()
	target := filepath.Join(d, "real")
	link := filepath.Join(d, "config")
	os.WriteFile(target, []byte("x\n"), 0o600)
	os.Symlink(target, link)
	s, err := Prepare(link)
	if err != nil {
		t.Fatal(err)
	}
	rt, _ := filepath.EvalSymlinks(target)
	if s.Path != rt {
		t.Errorf("path = %s, want %s", s.Path, rt)
	}
}

func TestCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nvim -u NONE")
	c, _ := Command("/tmp/c", 12)
	if got := strings.Join(c.Args, " "); got != "nvim -u NONE +12 /tmp/c" {
		t.Errorf("args = %s", got)
	}
	t.Setenv("EDITOR", "code -w")
	c, _ = Command("/tmp/c", 12)
	if got := strings.Join(c.Args, " "); got != "code -w /tmp/c" {
		t.Errorf("args = %s", got)
	}
}

func TestRelativePathBecomesAbsolute(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vi")
	d := t.TempDir()
	t.Chdir(d)
	for _, name := range []string{"-config", "+1"} {
		os.WriteFile(name, []byte("Host a\n"), 0o600)
		s, err := Prepare(name)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := Command(s.Path, 0)
		if last := c.Args[len(c.Args)-1]; !filepath.IsAbs(s.Path) || last != s.Path {
			t.Errorf("editor gets %q for %q", last, name)
		}
	}
}

func TestBackupHasTheBytesOfBefore(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	s, err := Prepare(f)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.Backup); !bytes.Equal(b, s.before) || string(b) != "Host a\n" {
		t.Errorf("backup %q, before %q", b, s.before)
	}
}

func TestNonRegularFileRefused(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	d := t.TempDir()
	pipe := filepath.Join(d, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skip(err)
	}
	if _, err := Prepare(pipe); err == nil { // a hang here fails by the test timeout
		t.Error("Prepare accepted a FIFO")
	}
	// The editor replaced the file with a FIFO: Finish must fail and keep the backup.
	f := filepath.Join(d, "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	s, err := Prepare(f)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(f)
	syscall.Mkfifo(f, 0o600)
	if changed, err := s.Finish(); err == nil || !changed {
		t.Errorf("changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(s.Backup); err != nil {
		t.Error("backup must stay")
	}
}

func TestOversizedFileRefused(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, make([]byte, backup.MaxSize+1), 0o600)
	if _, err := Prepare(f); err == nil {
		t.Error("want error")
	}
}

// backupNames returns the names in the backup folder, oldest first.
func backupNames(t *testing.T) []string {
	t.Helper()
	d, _ := backup.Dir()
	es, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// An edit that changes nothing must not cost an old backup: the app prunes
// only after it knows that the new backup stays.
func TestPruneOnlyAfterAChange(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	real, _ := filepath.EvalSymlinks(f)
	for i := 0; i < backup.Keep; i++ {
		if _, err := backup.CreateFromBytes(real, []byte("old\n")); err != nil {
			t.Fatal(err)
		}
	}
	full := backupNames(t)

	s, err := Prepare(f)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.Finish(); changed || err != nil {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := backupNames(t); strings.Join(got, ",") != strings.Join(full, ",") {
		t.Errorf("an edit without a change lost a backup: %d left, first %s, want first %s", len(got), got[0], full[0])
	}

	s, _ = Prepare(f)
	os.WriteFile(f, []byte("Host b\n"), 0o600)
	if changed, err := s.Finish(); !changed || err != nil {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got := backupNames(t)
	if len(got) != backup.Keep || got[0] != full[1] || got[len(got)-1] != filepath.Base(s.Backup) {
		t.Errorf("after a change: %d backups, first %s, last %s", len(got), got[0], got[len(got)-1])
	}
	if st, _ := os.Stat(s.Backup); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf("backup mode: %v", st)
	}
}
