package sshconfig

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A note is free text on one Host block. It is stored as tag comments in the
// tag stack above the Host line, one tag for each line of the note:
//
//	# sshwave: note=first line
//	# sshwave: note=second line
//
// OpenSSH reads the tags as comments.

// noteTagPrefix starts a note tag line, after optional indentation.
const noteTagPrefix = "# sshwave: note="

const (
	// MaxNoteLines is the largest number of lines in one note.
	MaxNoteLines = 10
	// MaxNoteLine is the longest note line, in bytes.
	MaxNoteLine = 200
)

// validNoteLine reports whether line can be stored in a tag line. A control
// character is refused, so a note can never add a line to the config.
func validNoteLine(line string) error {
	if !utf8.ValidString(line) {
		return errors.New("is not valid UTF-8")
	}
	for _, r := range line {
		if unicode.IsControl(r) {
			return errors.New("has a control character")
		}
	}
	if len(strings.Trim(line, " ")) > MaxNoteLine {
		return fmt.Errorf("is longer than %d bytes", MaxNoteLine)
	}
	return nil
}

// CleanNote returns the note in the form that is stored: no spaces around a
// line and no blank lines at the end. The result is empty for a note without
// text. It refuses a line that cannot be stored and a note with too many
// lines. It checks each line before it trims, so a tab or a line break is
// refused, not removed.
func CleanNote(lines []string) ([]string, error) {
	var out []string
	for i, l := range lines {
		if err := validNoteLine(l); err != nil {
			return nil, fmt.Errorf("note line %d %w", i+1, err)
		}
		out = append(out, strings.Trim(l, " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) > MaxNoteLines {
		return nil, fmt.Errorf("note has more than %d lines", MaxNoteLines)
	}
	return out, nil
}

// NoteTag returns the tag line for one note line, without indentation or line
// ending.
func NoteTag(line string) string { return noteTagPrefix + line }

// IsNoteTag reports whether line is a note tag line, also one with a text
// that ParseNoteTag refuses. The line can have indentation and a line ending.
func IsNoteTag(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, space), noteTagPrefix)
}

// ParseNoteTag returns the note line of a tag line. ok is false when line is
// not a note tag line or the text is not valid.
func ParseNoteTag(line string) (text string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimLeft(line, space), noteTagPrefix)
	if !found {
		return "", false
	}
	text = strings.Trim(rest, space+"\n")
	if validNoteLine(text) != nil {
		return "", false
	}
	return text, true
}

// IsTag reports whether line is a group tag or a note tag line. The tag
// stack of a Host line is the run of such lines directly above it.
func IsTag(line string) bool { return IsGroupTag(line) || IsNoteTag(line) }
