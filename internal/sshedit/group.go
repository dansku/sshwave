// Package sshedit changes an OpenSSH config file in place. Each change is the
// smallest possible edit of the raw bytes, with a backup first and an atomic
// replace of the file.
package sshedit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dansku/sshwave/internal/backup"
	"github.com/dansku/sshwave/internal/safefile"
	"github.com/dansku/sshwave/internal/sshconfig"
)

// ErrStale means the file is not the one that the caller parsed.
var ErrStale = errors.New("file changed on disk; reload")

// SetGroup sets the group of the Host block whose Host line is hostLine
// (1-based) in the file at path. An empty group removes the tag. hash is the
// sha256 of the file as the caller parsed it, and patterns are the patterns of
// that Host line. SetGroup writes nothing when the file or the line differs.
//
// It changes only group tag lines in the tag stack above the Host line, and
// leaves one group tag at most. All other bytes stay the same. It returns the
// path of the backup, or "" when the file already has the wanted content.
func SetGroup(path string, hash [sha256.Size]byte, hostLine int, patterns []string, group string) (string, error) {
	var tags []string
	if group != "" {
		if err := sshconfig.ValidGroupName(group); err != nil {
			return "", err
		}
		tags = []string{sshconfig.GroupTag(group)}
	}
	return apply(path, hash, hostLine, patterns, func(stack [][]byte, indent, eol []byte) [][]byte {
		// A new group tag goes to the top of the stack, above the notes.
		return setTags(stack, sshconfig.IsGroupTag, 0, tags, indent, eol)
	})
}

// SetNote sets the note of a Host block. An empty note, or one with only
// blank lines, removes it. The other arguments and the result are those of
// SetGroup. SetNote refuses a note that sshconfig.CleanNote refuses.
//
// It changes only note tag lines in the tag stack above the Host line. All
// other bytes stay the same.
func SetNote(path string, hash [sha256.Size]byte, hostLine int, patterns []string, note []string) (string, error) {
	note, err := sshconfig.CleanNote(note)
	if err != nil {
		return "", err
	}
	tags := make([]string, len(note))
	for i, l := range note {
		tags[i] = sshconfig.NoteTag(l)
	}
	return apply(path, hash, hostLine, patterns, func(stack [][]byte, indent, eol []byte) [][]byte {
		// New note tags go to the bottom of the stack, below a group tag.
		return setTags(stack, sshconfig.IsNoteTag, len(stack), tags, indent, eol)
	})
}

// A stackEdit returns the new tag stack of a Host line. stack holds the tag
// lines directly above the Host line, with their line endings. indent and eol
// are the indentation and the line ending for a tag line in an empty place.
type stackEdit func(stack [][]byte, indent, eol []byte) [][]byte

// apply changes the tag stack of one Host line in the file at path. It is the
// only way to the file: it checks the hash and the Host line, makes the backup,
// and replaces the file. The tags in edit must be valid: apply does not check
// them.
func apply(path string, hash [sha256.Size]byte, hostLine int, patterns []string, edit stackEdit) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	before, err := safefile.ReadAll(real, backup.MaxSize)
	if err != nil {
		return "", err
	}
	if sha256.Sum256(before) != hash {
		return "", ErrStale
	}
	after, err := withStack(before, hostLine, patterns, edit)
	if err != nil {
		return "", err
	}
	if bytes.Equal(after, before) {
		return "", nil
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	bak, err := backup.CreateFromBytes(real, before)
	if err != nil {
		return "", fmt.Errorf("backup failed, nothing written: %w", err)
	}
	if err := replaceFile(real, after, st.Mode().Perm()); err != nil {
		os.Remove(bak) // the file did not change
		return "", err
	}
	backup.Prune(real) // only now: the write is done and the new backup stays
	return bak, nil
}

// withStack returns data with the tag stack of one Host line changed by edit.
func withStack(data []byte, hostLine int, patterns []string, edit stackEdit) ([]byte, error) {
	// Split on \n only, as the parser does, so the line numbers agree.
	lines := bytes.SplitAfter(data, []byte("\n"))
	i := hostLine - 1
	if i < 0 || i >= len(lines) {
		return nil, fmt.Errorf("line %d is not in the file", hostLine)
	}
	host := lines[i]
	if got, ok := sshconfig.HostPatterns(string(bytes.TrimSuffix(host, []byte("\n")))); !ok || !slices.Equal(got, patterns) {
		return nil, fmt.Errorf("line %d is not the expected Host line", hostLine)
	}
	start := i
	for start > 0 && sshconfig.IsTag(string(lines[start-1])) {
		start--
	}
	eol := endingOf(host)
	if len(eol) == 0 { // the Host line is the last line, without a newline
		eol = []byte("\n")
		if i > 0 {
			eol = endingOf(lines[i-1])
		}
	}
	stack := edit(lines[start:i], indentOf(host), eol)
	return bytes.Join(slices.Concat(lines[:start], stack, lines[i:]), nil), nil
}

// setTags returns stack with all lines of one kind replaced by tags. is tells
// the kind. The tags go to the place of the first such line, with its
// indentation and line ending. When the stack has no such line, they go to
// index at, with indent and eol.
func setTags(stack [][]byte, is func(string) bool, at int, tags []string, indent, eol []byte) [][]byte {
	var out [][]byte
	found := false
	for _, line := range stack {
		if !is(string(line)) {
			out = append(out, line)
			continue
		}
		if !found {
			found, at, indent, eol = true, len(out), indentOf(line), endingOf(line)
		}
	}
	var lines [][]byte
	for _, tag := range tags {
		lines = append(lines, slices.Concat(indent, []byte(tag), eol))
	}
	return slices.Concat(out[:at], lines, out[at:])
}

// indentOf returns the leading spaces and tabs of line.
func indentOf(line []byte) []byte {
	return line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
}

// endingOf returns the line ending of line: "\r\n", "\n", or nothing.
func endingOf(line []byte) []byte {
	switch {
	case bytes.HasSuffix(line, []byte("\r\n")):
		return []byte("\r\n")
	case bytes.HasSuffix(line, []byte("\n")):
		return []byte("\n")
	}
	return nil
}

// replaceFile writes data to a temporary file next to path and renames it
// over path. On an error it removes the temporary file and path is unchanged.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	// CreateTemp opens with O_EXCL and mode 0600, so it never follows a
	// symlink and never opens an existing file.
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(dir); err == nil { // make the rename durable
		d.Sync()
		d.Close()
	}
	return nil
}
