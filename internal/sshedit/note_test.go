package sshedit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dansku/sshwave/internal/sshconfig"
)

// setNote loads f as the app does and sets the note of alias. Lines of the
// note are separated by "|". "" is no note.
func setNote(t *testing.T, f, alias, note string) (string, error) {
	t.Helper()
	var lines []string
	if note != "" {
		lines = strings.Split(note, "|")
	}
	c := sshconfig.Load(f)
	for _, h := range c.Hosts {
		if h.Alias == alias {
			return SetNote(f, c.Hash, h.Block.Line, h.Block.Patterns, lines)
		}
	}
	t.Fatalf("no host %s", alias)
	return "", nil
}

func backups(t *testing.T) int {
	t.Helper()
	es, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "sshwave", "backups"))
	return len(es)
}

func TestSetNoteEdits(t *testing.T) {
	for _, tc := range []struct{ name, before, note, after string }{
		{"insert", "Host z\n\nHost a b\n  User u\n", "one", "Host z\n\n# sshwave: note=one\nHost a b\n  User u\n"},
		{"insert lines", "Host a\n", "one||three", "# sshwave: note=one\n# sshwave: note=\n# sshwave: note=three\nHost a\n"},
		{"insert trims", "Host a\n", "  one  |two| | ", "# sshwave: note=one\n# sshwave: note=two\nHost a\n"},
		{"insert CRLF", "Host z\r\nHost a\r\n  User u\r\n", "one|two", "Host z\r\n# sshwave: note=one\r\n# sshwave: note=two\r\nHost a\r\n  User u\r\n"},
		{"insert in a mixed file", "Host z\r\nHost a\n", "one", "Host z\r\n# sshwave: note=one\nHost a\n"},
		{"insert keeps the indent", "Host z\n \t Host a\n", "one|two", "Host z\n \t # sshwave: note=one\n \t # sshwave: note=two\n \t Host a\n"},
		{"insert no final newline", "Host z\nHost a", "one|two", "Host z\n# sshwave: note=one\n# sshwave: note=two\nHost a"},
		{"insert no final newline CRLF", "Host z\r\nHost a", "one", "Host z\r\n# sshwave: note=one\r\nHost a"},
		{"insert one line no newline", "Host a", "one", "# sshwave: note=one\nHost a"},
		{"insert below a comment", "# plain\nHost a\n", "one", "# plain\n# sshwave: note=one\nHost a\n"},
		{"insert below a far note", "# sshwave: note=far\n\nHost a\n", "one", "# sshwave: note=far\n\n# sshwave: note=one\nHost a\n"},
		{"insert below the group", "# sshwave: group=G\nHost a\n", "one|two", "# sshwave: group=G\n# sshwave: note=one\n# sshwave: note=two\nHost a\n"},
		{"insert below the group CRLF", "\t# sshwave: group=G\r\nHost a\r\n", "one", "\t# sshwave: group=G\r\n# sshwave: note=one\r\nHost a\r\n"},
		{"insert with odd bytes", "Host z\t \n\x00\xff\n\n\nHost=a  # c\n\tUser  u\n\n", "see #42 = ok", "Host z\t \n\x00\xff\n\n\n# sshwave: note=see #42 = ok\nHost=a  # c\n\tUser  u\n\n"},
		{"replace", "Host z\n# sshwave: note=old\nHost a\n  User u\n", "new", "Host z\n# sshwave: note=new\nHost a\n  User u\n"},
		{"replace with more", "# sshwave: note=old\nHost a\n", "one|two|three", "# sshwave: note=one\n# sshwave: note=two\n# sshwave: note=three\nHost a\n"},
		{"replace with fewer", "# sshwave: note=1\n# sshwave: note=2\n# sshwave: note=3\nHost a\n", "one", "# sshwave: note=one\nHost a\n"},
		{"replace CRLF", "# sshwave: note=old\r\n# sshwave: note=old2\r\nHost a\r\n", "one|two", "# sshwave: note=one\r\n# sshwave: note=two\r\nHost a\r\n"},
		{"replace keeps the tag indent", "\t# sshwave: note=old  \nHost a\n", "one|two", "\t# sshwave: note=one\n\t# sshwave: note=two\nHost a\n"},
		{"replace no final newline", "# sshwave: note=old\nHost a", "new", "# sshwave: note=new\nHost a"},
		{"replace below the group", "# sshwave: group=G\n# sshwave: note=old\nHost a\n", "new", "# sshwave: group=G\n# sshwave: note=new\nHost a\n"},
		{"replace above the group", "# sshwave: note=old\n# sshwave: group=G\nHost a\n", "one|two", "# sshwave: note=one\n# sshwave: note=two\n# sshwave: group=G\nHost a\n"},
		{"replace around the group", "# sshwave: note=1\n# sshwave: group=G\n# sshwave: note=2\nHost a\n", "one|two", "# sshwave: note=one\n# sshwave: note=two\n# sshwave: group=G\nHost a\n"},
		{"replace bad tags", "# sshwave: note=a\x1bb\n# sshwave: note=" + strings.Repeat("x", 201) + "\n# sshwave: group=G\n# sshwave: note=a\tb\nHost a\n", "new", "# sshwave: note=new\n# sshwave: group=G\nHost a\n"},
		{"replace too many", strings.Repeat("# sshwave: note=n\n", 12) + "Host a\n", "new", "# sshwave: note=new\nHost a\n"},
		{"clear", "Host z\n# sshwave: note=1\n# sshwave: note=2\nHost a\n  User u\n", "", "Host z\nHost a\n  User u\n"},
		{"clear with blank lines", "# sshwave: note=old\nHost a\n", " | ", "Host a\n"},
		{"clear CRLF", "Host z\r\n# sshwave: note=old\r\nHost a\r\n", "", "Host z\r\nHost a\r\n"},
		{"clear first line", "# sshwave: note=old\nHost a", "", "Host a"},
		{"clear keeps the group", "# sshwave: note=1\n# sshwave: group=G\n# sshwave: note=2\nHost a\n", "", "# sshwave: group=G\nHost a\n"},
		{"clear bad tags", "# sshwave: note=a\x1bb\nHost a\n", "", "Host a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := write(t, tc.before)
			bak, err := setNote(t, f, "a", tc.note)
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
			// The parser reads back what the writer wrote, and the same note
			// again changes nothing.
			want, _ := sshconfig.CleanNote(strings.Split(tc.note, "|"))
			c := sshconfig.Load(f)
			for _, h := range c.Hosts {
				if h.Alias == "a" && strings.Join(h.Block.Note, "|") != strings.Join(want, "|") {
					t.Errorf("parsed note %q, want %q", h.Block.Note, want)
				}
			}
			if bak, err := setNote(t, f, "a", tc.note); err != nil || bak != "" {
				t.Errorf("the same note again: bak=%q err=%v", bak, err)
			}
		})
	}
}

// TestSetNoteChangesOnlyNoteLines compares the bytes around the edit in a
// file with many kinds of lines.
func TestSetNoteChangesOnlyNoteLines(t *testing.T) {
	src, err := os.ReadFile("../../testdata/ssh-notes/config")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		alias, note string
		first, old  int // 0-based index of the first note line, and the number of old note lines
	}{
		{"scratch", "one|two", 23, 0},       // insert above the Host line
		{"production-api", "one", 4, 3},     // the group tag above stays
		{"staging-api", "one|two|3", 11, 1}, // the group tag below stays
		{"raspberry-pi", "", 17, 1},
	} {
		f := write(t, string(src))
		if _, err := setNote(t, f, tc.alias, tc.note); err != nil {
			t.Fatal(err)
		}
		var added [][]byte
		if tc.note != "" {
			for _, l := range strings.Split(tc.note, "|") {
				added = append(added, []byte("# sshwave: note="+l+"\n"))
			}
		}
		before := bytes.SplitAfter(src, []byte("\n"))
		want := bytes.Join(before[:tc.first], nil)
		want = append(want, bytes.Join(added, nil)...)
		want = append(want, bytes.Join(before[tc.first+tc.old:], nil)...)
		if got := read(t, f); got != string(want) {
			t.Errorf("%s: bytes outside the note lines changed\n got %q\nwant %q", tc.alias, got, want)
		}
	}
}

func TestSetNoteSameNoteWritesNothing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, tc := range [][2]string{
		{"# sshwave: note=one\n# sshwave: note=\n# sshwave: note=three\nHost a\n", "one||three"},
		{"# sshwave: group=G\n# sshwave: note=one\nHost a\n", " one | "},
		{"# sshwave: note=one\r\nHost a\r\n", "one"},
		{"# sshwave: group=G\nHost a\n", ""},
		{"Host a\n", " | "},
	} {
		f := write(t, tc[0])
		st, _ := os.Stat(f)
		bak, err := setNote(t, f, "a", tc[1])
		if err != nil || bak != "" {
			t.Errorf("bak=%q err=%v", bak, err)
		}
		if now, _ := os.Stat(f); !os.SameFile(st, now) || read(t, f) != tc[0] {
			t.Error("the file was replaced")
		}
	}
	if n := backups(t); n != 0 {
		t.Errorf("%d backups for no change", n)
	}
}

func TestSetNoteRefusesBadNotes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const text = "# sshwave: note=old\nHost a\n  User u\n"
	f := write(t, text)
	c := sshconfig.Load(f)
	b := c.Hosts[0].Block
	ten := strings.Split(strings.Repeat("n,", 9)+"n", ",")
	for _, note := range [][]string{
		{"x\nProxyCommand evil"}, {"x\rProxyCommand evil"}, {"x\r\nHost *"}, {"\nx"}, {"x\n"}, {"x\r"},
		{"ok", "x\nHost *\n  ProxyCommand evil"},
		{"a\x00b"}, {"a\x1bb"}, {"a\tb"}, {"\tx"}, {"a\u0085b"}, {"\x7f"}, {"\xff"},
		{strings.Repeat("x", 201)}, {"ok", strings.Repeat("é", 101)}, append(ten, "n"),
	} {
		if _, err := SetNote(f, c.Hash, b.Line, b.Patterns, note); err == nil {
			t.Errorf("%q accepted", note)
		}
		if got := read(t, f); got != text {
			t.Fatalf("%q changed the file: %q", note, got)
		}
	}
	if es, _ := os.ReadDir(os.Getenv("XDG_DATA_HOME")); len(es) != 0 {
		t.Error("a refused note touched the backup folder")
	}
	// A refused note is refused before the file is read.
	if _, err := SetNote(filepath.Join(t.TempDir(), "none"), c.Hash, 1, nil, []string{"a\nb"}); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Errorf("err = %v", err)
	}
	// The limits themselves are accepted.
	ten[0] = strings.Repeat("x", 200)
	if _, err := SetNote(f, c.Hash, b.Line, b.Patterns, ten); err != nil {
		t.Errorf("10 lines with 200 bytes refused: %v", err)
	}
	if got := read(t, f); strings.Count(got, "\n") != 12 || !strings.HasSuffix(got, "# sshwave: note=n\nHost a\n  User u\n") {
		t.Errorf("file = %q", got)
	}
}

func TestSetNoteStaleFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := write(t, "Host a\n")
	c := sshconfig.Load(f)
	b := c.Hosts[0].Block
	const changed = "Host a\n  User u\n" // the Host line is still the same
	os.WriteFile(f, []byte(changed), 0o600)
	if _, err := SetNote(f, c.Hash, b.Line, b.Patterns, []string{"n"}); !errors.Is(err, ErrStale) {
		t.Errorf("err = %v", err)
	}
	if read(t, f) != changed {
		t.Error("a stale file was written")
	}
	if es, _ := os.ReadDir(os.Getenv("XDG_DATA_HOME")); len(es) != 0 {
		t.Error("a stale file got a backup")
	}
}

func TestSetNoteHostLineMismatch(t *testing.T) {
	const text = "# c\nHost a b\n  User u\nHost c\n"
	f := write(t, text)
	hash := sha256.Sum256([]byte(text))
	for _, tc := range []struct {
		line     int
		patterns []string
	}{
		{0, []string{"a", "b"}}, {1, []string{"a", "b"}}, {3, []string{"a", "b"}}, {5, []string{"a", "b"}}, {99, []string{"a", "b"}},
		{2, []string{"a"}}, {2, []string{"b", "a"}}, {2, nil}, {4, []string{"a", "b"}},
	} {
		if _, err := SetNote(f, hash, tc.line, tc.patterns, []string{"n"}); err == nil {
			t.Errorf("line %d %v accepted", tc.line, tc.patterns)
		}
		if read(t, f) != text {
			t.Fatalf("line %d %v changed the file", tc.line, tc.patterns)
		}
	}
	if _, err := SetNote(f, hash, 2, []string{"a", "b"}, []string{"n"}); err != nil {
		t.Error(err)
	}
}

func TestSetNoteKeepsTheMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o644, 0o400, 0o640} {
		f := write(t, "Host a\n")
		os.Chmod(f, mode)
		if _, err := setNote(t, f, "a", "n"); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(f); st.Mode().Perm() != mode {
			t.Errorf("mode %v, want %v", st.Mode().Perm(), mode)
		}
	}
}

func TestSetNoteThroughSymlink(t *testing.T) {
	d := t.TempDir()
	target := write(t, "Host a\n") // in another folder
	link := filepath.Join(d, "config")
	os.Symlink(target, link)
	if _, err := setNote(t, link, "a", "n"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
	if got := read(t, target); got != "# sshwave: note=n\nHost a\n" {
		t.Errorf("target = %q", got)
	}
	if names(t, link) != "config" || names(t, target) != "config" {
		t.Errorf("folders have %s and %s", names(t, link), names(t, target))
	}
}

func TestSetNoteFailedWrite(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const text = "Host a\n"
	f := write(t, text)
	dir := filepath.Dir(f)
	os.Chmod(dir, 0o500) // no temporary file can be made
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := setNote(t, f, "a", "n"); err == nil {
		t.Fatal("want an error")
	}
	if read(t, f) != text || names(t, f) != "config" {
		t.Errorf("file %q, folder %s", read(t, f), names(t, f))
	}
	if n := backups(t); n != 0 {
		t.Error("the backup of a failed write must not stay")
	}
}

func TestSetNoteBackupFailureWritesNothing(t *testing.T) {
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
	_, err := setNote(t, f, "a", "n")
	if err == nil || !strings.Contains(err.Error(), "backup failed") {
		t.Fatalf("err = %v", err)
	}
	if now, _ := os.Stat(f); !os.SameFile(st, now) || read(t, f) != text || names(t, f) != "config" {
		t.Error("the file changed without a backup")
	}
}

// TestSetGroupInAStack checks that a group change leaves one group tag and
// does not touch the note tags.
func TestSetGroupInAStack(t *testing.T) {
	const n1, n2 = "# sshwave: note=one\n", "# sshwave: note=two\n"
	for _, tc := range []struct{ name, before, group, after string }{
		{"two group tags", "# sshwave: group=A\n# sshwave: group=B\nHost a\n", "C", "# sshwave: group=C\nHost a\n"},
		{"two group tags, same group", "# sshwave: group=A\n# sshwave: group=B\nHost a\n", "A", "# sshwave: group=A\nHost a\n"},
		{"two group tags, clear", "# sshwave: group=A\n# sshwave: group=B\nHost a\n", "", "Host a\n"},
		{"group tags around notes", "# sshwave: group=\n" + n1 + "# sshwave: group=B\n" + n2 + "# sshwave: group=C\nHost a\n", "D", "# sshwave: group=D\n" + n1 + n2 + "Host a\n"},
		{"group tags around notes, clear", n1 + "# sshwave: group=A\n" + n2 + "# sshwave: group=B\nHost a\n", "", n1 + n2 + "Host a\n"},
		{"insert above notes", "Host z\n" + n1 + n2 + "Host a\n", "G", "Host z\n# sshwave: group=G\n" + n1 + n2 + "Host a\n"},
		{"insert above notes CRLF", "\t# sshwave: note=one\r\n  Host a", "G", "  # sshwave: group=G\r\n\t# sshwave: note=one\r\n  Host a"},
		{"replace above notes", "# sshwave: group=A\n" + n1 + "Host a\n", "G", "# sshwave: group=G\n" + n1 + "Host a\n"},
		{"replace below notes", n1 + "\t# sshwave: group=A\r\nHost a\n", "G", n1 + "\t# sshwave: group=G\r\nHost a\n"},
		{"clear below notes", n1 + "# sshwave: group=A\nHost a\n", "", n1 + "Host a\n"},
		{"far tags stay", "# sshwave: group=A\n" + n1 + "\n# sshwave: group=B\nHost a\n", "", "# sshwave: group=A\n" + n1 + "\nHost a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := write(t, tc.before)
			if _, err := set(t, f, "a", tc.group); err != nil {
				t.Fatal(err)
			}
			if got := read(t, f); got != tc.after {
				t.Errorf("file\n got %q\nwant %q", got, tc.after)
			}
			if n := strings.Count(read(t, f), "group="); (tc.group == "" && n != strings.Count(tc.after, "group=")) || (tc.group != "" && n != 1) {
				t.Errorf("%d group tags", n)
			}
		})
	}
}
