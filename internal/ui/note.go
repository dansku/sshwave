package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dansku/sshwave/internal/sshconfig"
	"github.com/dansku/sshwave/internal/sshedit"
)

// startNote opens the note dialog for the selected host, with its note as the
// text, or sets a status that says why not.
func (m *Model) startNote() {
	h, ok := m.tagHost("edit its note")
	if !ok {
		return
	}
	b := h.Block
	nt := noteState{alias: h.Alias, line: b.Line, patterns: b.Patterns, from: b.Note, lines: slices.Clone(b.Note)}
	if len(nt.lines) == 0 {
		nt.lines = []string{""}
	}
	nt.row = len(nt.lines) - 1
	nt.col = len([]rune(nt.lines[nt.row]))
	m.nt, m.dlg = nt, dlgNote
}

// insert puts text at the cursor. A line break in text starts a new line and
// a tab becomes four spaces. Any other control character stays in the text,
// so the dialog can show why the note is refused.
func (nt *noteState) insert(text string) {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(text)
	nt.lines = slices.Clone(nt.lines) // a copy of the model must keep its text
	rs := []rune(nt.lines[nt.row])
	parts := strings.Split(string(rs[:nt.col])+text, "\n")
	last := len(parts) - 1
	rest := string(rs[nt.col:])
	nt.col = len([]rune(parts[last]))
	parts[last] += rest
	nt.lines = slices.Replace(nt.lines, nt.row, nt.row+1, parts...)
	nt.row += last
}

// backspace removes the rune before the cursor. At the start of a line it
// joins the line to the one above.
func (nt *noteState) backspace() {
	nt.lines = slices.Clone(nt.lines)
	rs := []rune(nt.lines[nt.row])
	switch {
	case nt.col > 0:
		nt.lines[nt.row] = string(rs[:nt.col-1]) + string(rs[nt.col:])
		nt.col--
	case nt.row > 0:
		nt.col = len([]rune(nt.lines[nt.row-1]))
		nt.lines[nt.row-1] += nt.lines[nt.row]
		nt.lines = slices.Delete(nt.lines, nt.row, nt.row+1)
		nt.row--
	}
}

// moveTo puts the cursor on a row and a column, both cut to the text.
func (nt *noteState) moveTo(row, col int) {
	nt.row = max(0, min(len(nt.lines)-1, row))
	nt.col = max(0, min(len([]rune(nt.lines[nt.row])), col))
}

// problem returns why the text cannot be saved, or "".
func (nt noteState) problem() string {
	if _, err := sshconfig.CleanNote(nt.lines); err != nil {
		return err.Error()
	}
	return ""
}

func (m Model) noteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	nt := &m.nt
	switch msg.String() {
	case "esc":
		m.dlg = dlgNone
	case "ctrl+s":
		return m.confirmNote()
	case "enter":
		nt.insert("\n")
	case "tab":
		nt.insert("\t")
	case "backspace":
		nt.backspace()
	case "up":
		nt.moveTo(nt.row-1, nt.col)
	case "down":
		nt.moveTo(nt.row+1, nt.col)
	case "left":
		if nt.col == 0 && nt.row > 0 {
			nt.moveTo(nt.row-1, len(nt.lines[nt.row-1])) // cut to the end
		} else {
			nt.moveTo(nt.row, nt.col-1)
		}
	case "right":
		if nt.col == len([]rune(nt.lines[nt.row])) && nt.row < len(nt.lines)-1 {
			nt.moveTo(nt.row+1, 0)
		} else {
			nt.moveTo(nt.row, nt.col+1)
		}
	case "home", "ctrl+a":
		nt.moveTo(nt.row, 0)
	case "end", "ctrl+e":
		nt.moveTo(nt.row, len(nt.lines[nt.row])) // cut to the end
	default:
		if msg.Text != "" {
			nt.insert(msg.Text)
		}
	}
	return m, nil
}

// confirmNote closes the dialog and returns the command that writes the note.
// A note that cannot be stored keeps the dialog open: it shows the reason.
// The write puts the app in the edit state, as a move does.
func (m Model) confirmNote() (tea.Model, tea.Cmd) {
	nt := m.nt
	note, err := sshconfig.CleanNote(nt.lines)
	if err != nil {
		return m, nil
	}
	m.dlg = dlgNone
	if slices.Equal(note, nt.from) {
		m.setStatus(stInfo, "no changes")
		return m, nil
	}
	m.resetEff()
	m.editing = true
	path, hash := m.path, m.cfg.Hash
	return m, func() tea.Msg {
		bak, err := sshedit.SetNote(path, hash, nt.line, nt.patterns, note)
		return notedMsg{cleared: len(note) == 0, backup: bak, err: err}
	}
}
