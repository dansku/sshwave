//go:build unix

package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func TestCreateRoundTripAndPerms(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	src := filepath.Join(t.TempDir(), "config")
	data := []byte("Host a\r\n  User x \n\n#odd\tbytes\x00")
	os.WriteFile(src, data, 0o600)
	b, err := Create(src)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(b)
	if !bytes.Equal(got, data) {
		t.Error("backup differs from source")
	}
	st, _ := os.Stat(b)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("file perm %v", st.Mode().Perm())
	}
	d, _ := Dir()
	ds, _ := os.Stat(d)
	if ds.Mode().Perm() != 0o700 {
		t.Errorf("dir perm %v", ds.Mode().Perm())
	}
}

func TestPrune(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	src := filepath.Join(t.TempDir(), "config")
	os.WriteFile(src, []byte("x"), 0o600)
	var made []string
	for i := 0; i < Keep+5; i++ {
		b, err := Create(src)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, filepath.Base(b))
	}
	other := filepath.Join(t.TempDir(), "config") // same base name, another path
	if _, err := CreateFromBytes(other, []byte("y")); err != nil {
		t.Fatal(err)
	}
	d, _ := Dir()
	// Create alone removes nothing.
	if e, _ := os.ReadDir(d); len(e) != Keep+6 {
		t.Fatalf("Create pruned: %d backups, want %d", len(e), Keep+6)
	}
	Prune(src)
	e, _ := os.ReadDir(d)
	var kept []string
	for _, x := range e {
		if strings.HasSuffix(x.Name(), suffixFor(src)) {
			kept = append(kept, x.Name())
		}
	}
	// The newest Keep stay, and the backup of the other path stays.
	if len(e) != Keep+1 || strings.Join(kept, ",") != strings.Join(made[5:], ",") {
		t.Errorf("kept %d of src and %d in all, want %d and %d", len(kept), len(e), Keep, Keep+1)
	}
}

func TestMissingSourceFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := Create(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("want error")
	}
}

func count(t *testing.T, suffix string) int {
	t.Helper()
	d, _ := Dir()
	e, _ := os.ReadDir(d)
	n := 0
	for _, x := range e {
		if strings.HasSuffix(x.Name(), suffix) {
			n++
		}
	}
	return n
}

func TestNamesDoNotCollide(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, pair := range [][2]string{{"/a/b_c", "/a_b/c"}, {"/x/config", "/y/config"}} {
		if suffixFor(pair[0]) == suffixFor(pair[1]) {
			t.Fatalf("%v share a suffix", pair)
		}
		if _, err := CreateFromBytes(pair[0], []byte("keep")); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < Keep+3; i++ { // pruning the second path must keep the first
			if _, err := CreateFromBytes(pair[1], []byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		Prune(pair[1])
		if a, b := count(t, suffixFor(pair[0])), count(t, suffixFor(pair[1])); a != 1 || b != Keep {
			t.Errorf("%v: kept %d and %d, want 1 and %d", pair, a, b, Keep)
		}
	}
}

func TestLongNameIsCut(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	b, err := CreateFromBytes("/d/"+strings.Repeat("é", 200), []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if name := filepath.Base(b); len(name) > 255 || !utf8.ValidString(name) {
		t.Errorf("bad name (%d bytes)", len(name))
	}
}

func TestRelativeXDGIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "relative/dir")
	if d, _ := Dir(); d != filepath.Join(home, ".local", "share", "sshwave", "backups") {
		t.Errorf("dir = %s", d)
	}
}

func TestLooseDirIsTightened(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	dir := filepath.Join(base, "sshwave", "backups")
	os.MkdirAll(dir, 0o777)
	os.Chmod(filepath.Dir(dir), 0o777) // MkdirAll obeys the umask
	os.Chmod(dir, 0o777)
	if _, err := CreateFromBytes("/x/config", []byte("x")); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{dir, filepath.Dir(dir)} {
		if st, _ := os.Stat(d); st.Mode().Perm() != 0o700 {
			t.Errorf("%s perm %v", d, st.Mode().Perm())
		}
	}
}

func TestSymlinkedDirRefused(t *testing.T) {
	for _, name := range []string{"sshwave", "sshwave/backups"} {
		base, elsewhere := t.TempDir(), t.TempDir()
		t.Setenv("XDG_DATA_HOME", base)
		os.MkdirAll(filepath.Dir(filepath.Join(base, name)), 0o700)
		os.Symlink(elsewhere, filepath.Join(base, name))
		if _, err := CreateFromBytes("/x/config", []byte("x")); err == nil {
			t.Errorf("symlinked %s accepted", name)
		}
		if e, _ := os.ReadDir(elsewhere); len(e) != 0 {
			t.Errorf("something was made through the symlink %s", name)
		}
	}
}

func TestCreateRefusesFifo(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip(err)
	}
	if _, err := Create(p); err == nil { // a hang here fails by the test timeout
		t.Error("want error")
	}
}
