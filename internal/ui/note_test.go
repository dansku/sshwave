package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dansku/sshwave/internal/sshconfig"
)

// noteFixture is the notes fixture copied to ~/.ssh/config of the test home. TestMain sets it.
var noteFixture string

var ctrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

// These tests save only in temporary files. The fixture is read-only.

func TestGoldenNotes(t *testing.T) {
	m := selectAlias(t, loadedFrom(noteFixture, 80, 24), "production-api")
	golden(t, "notes_80x24", ansi.Strip(m.render()))

	m = selectAlias(t, loadedFrom(noteFixture, 120, 40), "github")
	golden(t, "notes_120x40", ansi.Strip(m.render()))

	m = selectAlias(t, loadedFrom(noteFixture, 60, 16), "staging-api")
	m.ensureVisible()
	golden(t, "notes_60x16", ansi.Strip(m.render()))

	m = keyRune(selectAlias(t, loadedFrom(noteFixture, 80, 24), "production-api"), 'n')
	if m.dlg != dlgNote {
		t.Fatal("n must open the note dialog")
	}
	golden(t, "notes_dialog_80x24", ansi.Strip(m.render()))

	m = keyRune(selectAlias(t, loadedFrom(noteFixture, 60, 16), "scratch"), 'n')
	golden(t, "notes_dialog_60x16", ansi.Strip(m.render()))
}

// TestScreenHeight checks that no dialog makes the screen taller than the
// terminal.
func TestScreenHeight(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 16}, {40, 10}} {
		w, h := size[0], size[1]
		base := selectAlias(t, loadedFrom(noteFixture, w, h), "production-api")
		note := keyRune(base, 'n')
		if note.dlg != dlgNote {
			t.Fatal("no note dialog")
		}
		long := note
		long.nt.insert(strings.Repeat("line\n", 30) + "\x1b[2J")
		help := keyRune(base, '?')
		views := map[string]Model{"list": base, "note": note, "long note": long}
		if h >= 16 { // the help needs 60x16
			views["help"] = help
		}
		for name, m := range views {
			lines := strings.Split(ansi.Strip(m.render()), "\n")
			if len(lines) != h {
				t.Errorf("%dx%d %s: %d lines", w, h, name, len(lines))
			}
			for _, l := range lines {
				if lipgloss.Width(l) > w {
					t.Errorf("%dx%d %s: line is %d cells: %q", w, h, name, lipgloss.Width(l), l)
				}
			}
		}
		if out := ansi.Strip(note.render()); w >= 60 && (!contains(out, "Plain text in your config. Do not store passwords.") || !contains(out, "3/10 lines")) {
			t.Errorf("%dx%d: the note dialog lacks the hint or the count:\n%s", w, h, out)
		}
	}
}

func TestNoteMarkerInList(t *testing.T) {
	m := loadedFrom(noteFixture, 120, 40)
	out := ansi.Strip(m.render())
	for alias, want := range map[string]bool{"github": true, "production-api": true, "prod": true, "staging-api": true, "raspberry-pi": true, "scratch": false} {
		if got := contains(out, alias+" ✎"); got != want {
			t.Errorf("%s: marker=%v, want %v:\n%s", alias, got, want, out)
		}
	}
	if contains(out, "Rebuilt each night") || contains(out, "*.internal ✎") {
		t.Errorf("the list shows note text, or a rule has a marker:\n%s", out)
	}
	// A long alias is cut. The marker and the right column stay.
	t.Setenv("HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("# sshwave: note=n\nHost a-very-long-alias-that-does-not-fit-in-the-list\n  HostName 192.168.100.200\n"), 0o600)
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 16}, {40, 10}} {
		out := ansi.Strip(loadedFrom(f, size[0], size[1]).render())
		var row string
		for _, l := range strings.Split(out, "\n") {
			if cell, _, _ := strings.Cut(strings.TrimPrefix(l, "│"), "│"); contains(cell, "a-very") {
				row = cell
			}
		}
		if !contains(row, "… ✎ ") || !contains(row, "192.168.") {
			t.Errorf("%v: row %q", size, row)
		}
		for _, l := range strings.Split(out, "\n") {
			if lipgloss.Width(l) > size[0] {
				t.Errorf("%v: line is %d cells", size, lipgloss.Width(l))
			}
		}
	}
}

func TestNoteInDetailPanel(t *testing.T) {
	m := selectAlias(t, loadedFrom(noteFixture, 80, 24), "production-api")
	out := ansi.Strip(m.render())
	for _, want := range []string{"group: Work", "✎ Primary API box. Ask team before a res…", "  Deploys run at 02:00 UTC.", "CONFIGURATION", "Host production-api prod", "allow and resolve"} {
		if !contains(out, want) {
			t.Errorf("the panel lacks %q:\n%s", want, out)
		}
	}
	if contains(out, "# sshwave:") {
		t.Errorf("the panel shows a tag line:\n%s", out)
	}
	// The note of an Included host shows. A rule shows no note.
	if out := ansi.Strip(selectAlias(t, m, "github").render()); !contains(out, "✎ Deploy key only") || !contains(out, "INCLUDED") {
		t.Errorf("included host:\n%s", out)
	}
	for i, r := range m.rows {
		if r.rule != nil {
			m.cursor = i
			if out := ansi.Strip(m.render()); contains(out, "does not count") {
				t.Errorf("a rule shows a note:\n%s", out)
			}
		}
	}

	// A tall note is cut, so the sections below it still fit.
	t.Setenv("HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	text := strings.Repeat("# sshwave: note=note line\n", 10) + "# sshwave: group=G\nHost a\n" + strings.Repeat("  User u\n", 9)
	os.WriteFile(f, []byte(text), 0o600)
	for size, more := range map[[2]int]string{{80, 24}: "… 8 more", {120, 40}: "… 4 more"} {
		m := selectAlias(t, loadedFrom(f, size[0], size[1]), "a")
		out := ansi.Strip(m.render())
		for _, want := range []string{"group: G", "✎ note line", more, "CONFIGURATION", "more lines", "EFFECTIVE", "allow and resolve"} {
			if !contains(out, want) {
				t.Errorf("%v: the panel lacks %q:\n%s", size, want, out)
			}
		}
	}
}

func TestNoteRefused(t *testing.T) {
	// An Included host: the notice names its file, and no dialog opens.
	m := keyRune(selectAlias(t, loadedFrom(noteFixture, 100, 30), "github"), 'n')
	if m.dlg != dlgNone || !contains(m.status, "git.conf") || !contains(m.status, "edits only") || m.statusKind != stErr {
		t.Errorf("included host: dlg=%d status=%q", m.dlg, m.status)
	}
	// A rule and a header have no note to set.
	m = loadedFrom(groupFixture, 100, 30)
	for i := range m.rows {
		if m.rows[i].host != nil {
			continue
		}
		m.cursor = i
		if n := keyRune(m, 'n'); n.dlg != dlgNone || !contains(n.status, "select a host to edit its note") {
			t.Errorf("row %d: dlg=%d status=%q", i, n.dlg, n.status)
		}
	}
	// A missing config.
	m = loadedFrom(filepath.Join(t.TempDir(), "none"), 100, 30)
	if m = keyRune(m, 'n'); m.dlg != dlgNone || !contains(m.status, "does not exist") {
		t.Errorf("missing config: dlg=%d status=%q", m.dlg, m.status)
	}
	// A config that was not read to its end has no hash.
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n# "+strings.Repeat("x", 1<<20)+"\n"), 0o600)
	m = loadedFrom(f, 100, 30)
	if m = keyRune(m, 'n'); m.dlg != dlgNone || !contains(m.status, "not fully read") {
		t.Errorf("truncated config: dlg=%d status=%q", m.dlg, m.status)
	}
}

func TestSearchMatchesNoteText(t *testing.T) {
	m := loadedFrom(noteFixture, 80, 24)
	m.folded[""] = true
	m.rebuild()
	if strings.Contains(labels(m), "raspberry-pi") {
		t.Fatal("Ungrouped is not folded")
	}
	// Only the note of raspberry-pi has these letters in this order. The
	// search shows its folded group.
	m = typeText(keyRune(m, '/'), "fan is loud")
	if got := labels(m); got != "[Ungrouped] raspberry-pi" {
		t.Errorf("search for note text: %s", got)
	}
	// A second line of a note, and the note of an Included host.
	m = typeText(keyRune(loadedFrom(noteFixture, 80, 24), '/'), "02:00 utc")
	if got := labels(m); got != "[Work] production-api prod" {
		t.Errorf("search for the second note line: %s", got)
	}
	m = typeText(keyRune(loadedFrom(noteFixture, 80, 24), '/'), "deploy key")
	if got := labels(m); got != "[Ungrouped] github" {
		t.Errorf("search for the note of an included host: %s", got)
	}
	// The other fields still match. A plain comment and a rule note do not.
	m = typeText(keyRune(loadedFrom(noteFixture, 80, 24), '/'), "10.0.0.99")
	if got := labels(m); got != "[Ungrouped] scratch" {
		t.Errorf("search for a host name: %s", got)
	}
	for _, q := range []string{"plain comment", "does not count"} {
		m = typeText(keyRune(loadedFrom(noteFixture, 80, 24), '/'), q)
		if got := labels(m); got != "" {
			t.Errorf("search for %q: %s", q, got)
		}
	}
}

func TestNoteFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	const before = "# sshwave: group=Work\nHost web\n  User u\n\nHost db db2\n  User d\n\nHost *\n  User x\n"
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte(before), 0o600)
	bakDir := filepath.Join(xdg, "sshwave", "backups")

	m := selectAlias(t, loadedFrom(f, 100, 30), "web")
	m.consent = true
	m.eff["web"] = &effState{}
	seq := m.seq

	m = keyRune(m, 'n')
	if m.dlg != dlgNote || len(m.nt.lines) != 1 || m.nt.lines[0] != "" {
		t.Fatalf("dialog: dlg=%d %+v", m.dlg, m.nt)
	}
	if out := ansi.Strip(m.render()); !contains(out, "╭─ Note ") || !contains(out, "Plain text in your config. Do not store passwords.") || !contains(out, "0/10 lines") {
		t.Fatalf("dialog text:\n%s", out)
	}
	// Save with no text and no old note: nothing to do.
	n, cmd := m.Update(ctrlS)
	if cmd != nil || n.(Model).editing || n.(Model).dlg != dlgNone || n.(Model).status != "no changes" {
		t.Errorf("empty note: cmd=%v status=%q", cmd != nil, n.(Model).status)
	}

	// Letters that are keys elsewhere are text here. Enter starts a line, a
	// tab becomes spaces.
	m = typeText(m, "q j? x m n /")
	m = press(m, tea.KeyEnter)
	m = press(m, tea.KeyEnter)
	m = typeText(press(typeText(m, "a"), tea.KeyTab), "b ")
	if got := strings.Join(m.nt.lines, "|"); got != "q j? x m n /||a    b " || m.dlg != dlgNote {
		t.Fatalf("typed %q", got)
	}
	// The cursor moves between lines, and backspace joins lines.
	m = press(press(press(m, tea.KeyUp), tea.KeyUp), tea.KeyEnd)
	if m.nt.row != 0 || m.nt.col != 12 {
		t.Fatalf("cursor %d:%d", m.nt.row, m.nt.col)
	}
	for range 5 {
		m = press(m, tea.KeyLeft)
	}
	m = typeText(m, "k ")
	m = press(press(m, tea.KeyDown), tea.KeyBackspace) // the empty line goes
	m = press(press(m, tea.KeyRight), tea.KeyEnter)    // a new line at the start of "a    b "
	m = typeText(press(m, tea.KeyUp), "second")
	if got := strings.Join(m.nt.lines, "|"); got != "q j? x k m n /|second|a    b " {
		t.Fatalf("after edits %q (cursor %d:%d)", got, m.nt.row, m.nt.col)
	}
	if out := ansi.Strip(m.render()); !contains(out, "second▏") || !contains(out, "3/10 lines · line 2: 6/200 bytes") {
		t.Fatalf("dialog text:\n%s", out)
	}

	// Esc closes the dialog and writes nothing.
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil || n.(Model).dlg != dlgNone || n.(Model).editing || !n.(Model).consent {
		t.Error("esc must only close the dialog")
	}
	if got, _ := os.ReadFile(f); string(got) != before {
		t.Fatalf("esc changed the file: %q", got)
	}

	// Ctrl+S writes. The write ends the consent, as a move does.
	n, cmd = m.Update(ctrlS)
	m = n.(Model)
	if m.dlg != dlgNone || !m.editing || m.consent || len(m.eff) != 0 || m.seq == seq {
		t.Fatalf("save must end the consent: editing=%v consent=%v eff=%d", m.editing, m.consent, len(m.eff))
	}
	if again := keyRune(m, 'n'); again.dlg != dlgNone {
		t.Error("n opened a dialog during a write")
	}
	if x := keyRune(keyRune(m, 'x'), 'y'); x.consent {
		t.Error("consent came back during a write")
	}
	m, cmd = run(t, m, cmd) // the write
	const noted = "# sshwave: group=Work\n# sshwave: note=q j? x k m n /\n# sshwave: note=second\n# sshwave: note=a    b\nHost web\n  User u\n\nHost db db2\n  User d\n\nHost *\n  User x\n"
	if got, _ := os.ReadFile(f); string(got) != noted {
		t.Fatalf("file = %q", got)
	}
	baks, _ := os.ReadDir(bakDir)
	if len(baks) != 1 {
		t.Fatalf("%d backups", len(baks))
	}
	if b, _ := os.ReadFile(filepath.Join(bakDir, baks[0].Name())); string(b) != before {
		t.Errorf("backup = %q", b)
	}
	if want := "note saved · backup: " + baks[0].Name(); m.status != want || m.statusKind != stOK {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	if !m.editing {
		t.Error("the edit state must last until the reload")
	}
	m, cmd = run(t, m, cmd) // the reload
	if cmd != nil || m.editing || m.consent || m.selectedAlias() != "web" || !contains(m.status, "note saved") {
		t.Errorf("after reload: cmd=%v editing=%v consent=%v alias=%q status=%q", cmd != nil, m.editing, m.consent, m.selectedAlias(), m.status)
	}
	if r, _ := m.current(); strings.Join(r.host.Block.Note, "|") != "q j? x k m n /|second|a    b" || r.host.Block.Group != "Work" {
		t.Errorf("block after reload: %+v", r.host.Block)
	}

	// The dialog opens with the note. The same text again writes nothing.
	m = keyRune(m, 'n')
	if got := strings.Join(m.nt.lines, "|"); got != "q j? x k m n /|second|a    b" || m.nt.row != 2 || m.nt.col != 6 {
		t.Fatalf("the dialog must hold the note: %q (cursor %d:%d)", got, m.nt.row, m.nt.col)
	}
	n, cmd = press(typeText(m, "  "), tea.KeyEnter).Update(ctrlS) // only blanks more
	if cmd != nil || n.(Model).editing || n.(Model).status != "no changes" {
		t.Errorf("same note: cmd=%v status=%q", cmd != nil, n.(Model).status)
	}

	// A note that cannot be stored keeps the dialog open and shows why.
	bad := typeText(m, strings.Repeat("é", 99))
	n, cmd = bad.Update(ctrlS)
	if out := ansi.Strip(n.(Model).render()); cmd != nil || n.(Model).dlg != dlgNote || !contains(out, "note line 3 is longer than 200 bytes") {
		t.Fatalf("long line accepted or no reason shown:\n%s", out)
	}
	bad = m
	for range 8 {
		bad = typeText(press(bad, tea.KeyEnter), "x")
	}
	n, cmd = bad.Update(ctrlS)
	if out := ansi.Strip(n.(Model).render()); cmd != nil || n.(Model).dlg != dlgNote || !contains(out, "note has more than 10 lines") {
		t.Fatalf("11 lines accepted or no reason shown:\n%s", out)
	}
	// A pasted line break is a new note line. It can add no config line.
	n, _ = m.Update(tea.PasteMsg{Content: "\r\nProxyCommand evil\n\tHost *"})
	n, cmd = n.(Model).Update(ctrlS)
	m, cmd = run(t, n.(Model), cmd)
	m, _ = run(t, m, cmd)
	pasted := strings.Replace(noted, "note=a    b\n", "note=a    b\n# sshwave: note=ProxyCommand evil\n# sshwave: note=Host *\n", 1)
	if got, _ := os.ReadFile(f); string(got) != pasted {
		t.Fatalf("file = %q", got)
	}
	if c := sshconfig.Load(f); len(c.Hosts) != 3 || len(c.Rules) != 1 || c.Hosts[0].Block.Get("ProxyCommand") != "" {
		t.Fatalf("the paste changed the config: %v", c.Hosts)
	}
	// Another control character is refused. The file stays as it is.
	n, _ = keyRune(m, 'n').Update(tea.PasteMsg{Content: "a\x1b]8;;http://evil\x07b"})
	n, cmd = n.(Model).Update(ctrlS)
	if out := ansi.Strip(n.(Model).render()); cmd != nil || n.(Model).dlg != dlgNote || !contains(out, "note line 5 has a control character") {
		t.Fatalf("control character accepted or no reason shown:\n%s", out)
	}
	if got, _ := os.ReadFile(f); string(got) != pasted {
		t.Fatalf("file changed: %q", got)
	}
	// A paste goes nowhere without the dialog.
	if n, cmd := m.Update(tea.PasteMsg{Content: "qqq"}); cmd != nil || n.(Model).dlg != dlgNone {
		t.Error("a paste outside the dialog did something")
	}

	// Remove all text and save: the note goes, the group tag stays.
	m = keyRune(m, 'n')
	for len(m.nt.lines) > 1 || m.nt.lines[0] != "" {
		m = press(m, tea.KeyBackspace)
	}
	n, cmd = m.Update(ctrlS)
	m, cmd = run(t, n.(Model), cmd)
	if got, _ := os.ReadFile(f); string(got) != before {
		t.Fatalf("file = %q, want the first content", got)
	}
	baks, _ = os.ReadDir(bakDir)
	if len(baks) != 3 || m.status != "note cleared · backup: "+baks[2].Name() {
		t.Errorf("%d backups, status %q", len(baks), m.status)
	}
	m, _ = run(t, m, cmd)
	if r, _ := m.current(); m.selectedAlias() != "web" || r.host.Block.Note != nil {
		t.Errorf("after clear: alias %q note %q", m.selectedAlias(), r.host.Block.Note)
	}

	// A second alias of a block: the cursor stays on it.
	m = keyRune(selectAlias(t, m, "db2"), 'n')
	n, cmd = typeText(m, "shared").Update(ctrlS)
	m, cmd = run(t, n.(Model), cmd)
	m, _ = run(t, m, cmd)
	if got, _ := os.ReadFile(f); !contains(string(got), "\n\n# sshwave: note=shared\nHost db db2\n") || m.selectedAlias() != "db2" {
		t.Errorf("file = %q, alias %q", got, m.selectedAlias())
	}
	if out := ansi.Strip(m.render()); !contains(out, "db ✎") || !contains(out, "db2 ✎") || contains(out, "web ✎") {
		t.Errorf("markers:\n%s", out)
	}
}

func TestNoteStaleFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host web\nHost db\n"), 0o600)
	m := typeText(keyRune(selectAlias(t, loadedFrom(f, 100, 30), "db"), 'n'), "text")
	const changed = "Host web\nHost db\n  User other\n"
	os.WriteFile(f, []byte(changed), 0o600) // another program edits the file
	n, cmd := m.Update(ctrlS)
	m, cmd = run(t, n.(Model), cmd)
	if got, _ := os.ReadFile(f); string(got) != changed {
		t.Fatalf("a stale file was written: %q", got)
	}
	if m.statusKind != stErr || !contains(m.status, "changed on disk") {
		t.Errorf("status = %q", m.status)
	}
	if es, _ := os.ReadDir(xdg); len(es) != 0 {
		t.Error("a backup was made")
	}
	// The reload ends the edit state, so the next try works on the new file.
	m, _ = run(t, m, cmd)
	if m.editing || m.selectedAlias() != "db" {
		t.Errorf("after reload: editing=%v alias=%q", m.editing, m.selectedAlias())
	}
	if m = keyRune(m, 'n'); m.dlg != dlgNote {
		t.Error("n must work after the reload")
	}
	// A reload while the dialog is open closes it: its lines are old.
	n, _ = m.Update(loadedMsg{sshconfig.Load(f)})
	if n.(Model).dlg != dlgNone {
		t.Error("a reload must close the note dialog")
	}
}

func TestNotesAreSanitized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const bidi = "n\u202egnp\u2066\u200fx"
	const evil = "\x1b]8;;http://evil\x07link\x1b]8;;\x07\x1b[2J\u009b2J\u202egnp\u2066\u200f\x7f"
	f := filepath.Join(t.TempDir(), "config")
	text := "# sshwave: note=" + bidi + "\n" +
		"# sshwave: note=hidden" + evil + "\n" + // a control character: not a note line
		"Host a\n  User u\nHost b\n"
	os.WriteFile(f, []byte(text), 0o600)
	m := selectAlias(t, loadedFrom(f, 120, 40), "a")
	if got := m.cfg.Hosts[0].Block.Note; len(got) != 1 || got[0] != bidi {
		t.Fatalf("note = %q", got)
	}
	views := map[string]Model{"panel": m}
	d := keyRune(m, 'n')
	views["dialog"] = d
	for name, v := range views {
		if plain := noControl(t, name, v.render()); !contains(plain, "n·gnp··x") || contains(plain, "hidden") {
			t.Errorf("%s: the note is not visible as text, or a refused line shows:\n%s", name, plain)
		}
	}
	// Hostile text in the dialog shows as text, with the reason.
	n, _ := keyRune(selectAlias(t, m, "b"), 'n').Update(tea.PasteMsg{Content: "typed" + evil})
	n, _ = n.(Model).Update(tea.KeyPressMsg{Text: "\x1b[2J\u202e"})
	n, cmd := n.(Model).Update(ctrlS)
	if cmd != nil || n.(Model).dlg != dlgNote {
		t.Fatal("the dialog must stay open")
	}
	plain := noControl(t, "input", n.(Model).render())
	if !contains(plain, "typed·]8;;http://evil·link") || !contains(plain, "·[2J") || !contains(plain, "note line 1 has a control character") {
		t.Errorf("the OSC 8 payload must show as text, with the reason:\n%s", plain)
	}
	if got, _ := os.ReadFile(f); string(got) != text {
		t.Errorf("file changed: %q", got)
	}
}
