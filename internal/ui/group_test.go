package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dansku/sshwave/internal/sshconfig"
)

const groupFixture = "../../testdata/ssh-groups/config"

func loadedFrom(path string, w, h int) Model {
	m := New(path)
	m.w, m.h = w, h
	next, _ := m.Update(loadedMsg{sshconfig.Load(path)})
	return next.(Model)
}

func press(m Model, code rune) Model {
	n, _ := m.Update(tea.KeyPressMsg{Code: code})
	return n.(Model)
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		n, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = n.(Model)
	}
	return m
}

// labels returns the rows as text: "[Group]" for a header, else the alias or
// the patterns.
func labels(m Model) string {
	var out []string
	for _, r := range m.rows {
		switch {
		case r.head:
			out = append(out, "["+groupLabel(r.group)+"]")
		case r.host != nil:
			out = append(out, r.host.Alias)
		default:
			out = append(out, strings.Join(r.rule.Patterns, " "))
		}
	}
	return strings.Join(out, " ")
}

// select moves the cursor to the host alias.
func selectAlias(t *testing.T, m Model, alias string) Model {
	t.Helper()
	m.reselect(rowID{false, alias})
	if m.selectedAlias() != alias {
		t.Fatalf("no row for %s in: %s", alias, labels(m))
	}
	return m
}

func TestGoldenGroups(t *testing.T) {
	m := selectAlias(t, loadedFrom(groupFixture, 80, 24), "production-api")
	golden(t, "groups_80x24", ansi.Strip(m.render()))

	m = selectAlias(t, loadedFrom(groupFixture, 60, 16), "staging-api")
	m.ensureVisible()
	golden(t, "groups_60x16", ansi.Strip(m.render()))

	// The move dialog. It needs a main file that the app can name the same
	// way on each machine, so the fixture path is enough.
	m = keyRune(selectAlias(t, loadedFrom(groupFixture, 80, 24), "scratch"), 'm')
	if m.dlg != dlgMove {
		t.Fatal("m must open the move dialog")
	}
	golden(t, "groups_move_80x24", ansi.Strip(m.render()))
}

func TestGroupRows(t *testing.T) {
	m := loadedFrom(groupFixture, 80, 24)
	want := "[Git] github [Work] production-api prod staging-api [Home] raspberry-pi [Ungrouped] scratch not-directly-above *.internal *"
	if got := labels(m); got != want {
		t.Errorf("rows\n got %s\nwant %s", got, want)
	}
	// Without a group in the config the list has no header row.
	if got := labels(loaded(80, 24)); strings.Contains(got, "[") {
		t.Errorf("header in a config without groups: %s", got)
	}
}

func TestFoldSkipsHosts(t *testing.T) {
	m := loadedFrom(groupFixture, 80, 24)
	m = keyRune(keyRune(m, 'j'), 'j') // header Git, github, header Work
	if r, _ := m.current(); !r.head || r.group != "Work" {
		t.Fatalf("cursor is not on Work: %s", labels(m))
	}
	m = press(m, tea.KeySpace)
	if got := labels(m); strings.Contains(got, "production-api") || !strings.Contains(got, "[Work] [Home]") {
		t.Errorf("Work is not folded: %s", got)
	}
	if r, _ := m.current(); !r.head || r.group != "Work" {
		t.Error("the cursor left the folded header")
	}
	if out := ansi.Strip(m.render()); !contains(out, "▸ Work · 3") || !contains(out, "▾ Home · 1") {
		t.Errorf("fold markers wrong:\n%s", out)
	}
	if m = keyRune(m, 'j'); m.rows[m.cursor].group != "Home" {
		t.Errorf("j must skip the folded hosts, cursor on %+v", m.rows[m.cursor])
	}
	if m = keyRune(keyRune(m, 'k'), 'k'); m.selectedAlias() != "github" {
		t.Errorf("k from Work must reach github, got %q", m.selectedAlias())
	}
	// Enter on the header unfolds. It starts no edit.
	m = keyRune(m, 'j')
	n, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	if cmd != nil || m.editing || !strings.Contains(labels(m), "[Work] production-api") {
		t.Errorf("enter on a header: cmd=%v editing=%v rows=%s", cmd != nil, m.editing, labels(m))
	}
	if out := ansi.Strip(m.render()); !contains(out, "↵  fold") || contains(out, "↵  edit") {
		t.Errorf("the footer must say what enter does on a header:\n%s", out)
	}
	// e on a header still starts an edit of the file.
	if cmd, starts := m.startEdit(); cmd == nil || !starts {
		t.Error("e on a header must start an edit")
	}
	// A reload keeps the folded state and the cursor on a header.
	m = press(m, tea.KeySpace)
	n, _ = m.Update(loadedMsg{sshconfig.Load(m.path)})
	m = n.(Model)
	if r, _ := m.current(); !r.head || r.group != "Work" || strings.Contains(labels(m), "prod") {
		t.Errorf("after reload: %+v rows=%s", r, labels(m))
	}
}

func TestSearchMatchesGroupAndIgnoresFold(t *testing.T) {
	m := loadedFrom(groupFixture, 80, 24)
	m.folded["Home"] = true
	m.rebuild()
	if strings.Contains(labels(m), "raspberry-pi") {
		t.Fatal("Home is not folded")
	}
	m = typeText(keyRune(m, '/'), "home")
	if got := labels(m); got != "[Home] raspberry-pi" {
		t.Errorf("search for a group name: %s", got)
	}
	if out := ansi.Strip(m.render()); !contains(out, "▾ Home · 1") {
		t.Errorf("a search shows groups unfolded:\n%s", out)
	}
	// Only groups with a match show, with the count of matches.
	m = loadedFrom(groupFixture, 80, 24)
	m = typeText(keyRune(m, '/'), "staging")
	if got := labels(m); got != "[Work] staging-api" || m.rows[0].count != 1 {
		t.Errorf("search for a host: %s (count %d)", got, m.rows[0].count)
	}
}

func TestMoveRefused(t *testing.T) {
	// An Included host: the notice names its file, and no dialog opens.
	m := keyRune(selectAlias(t, loadedFrom(groupFixture, 100, 30), "github"), 'm')
	if m.dlg != dlgNone || !contains(m.status, "git.conf") || !contains(m.status, "edits only") {
		t.Errorf("included host: dlg=%d status=%q", m.dlg, m.status)
	}
	// The group of an Included host still shows.
	if out := ansi.Strip(m.render()); !contains(out, "group: Git") {
		t.Errorf("group of an included host not shown:\n%s", out)
	}
	// A rule and a header have no group to set.
	m = loadedFrom(groupFixture, 100, 30)
	for i := range m.rows {
		if m.rows[i].host != nil {
			continue
		}
		m.cursor = i
		if n := keyRune(m, 'm'); n.dlg != dlgNone || n.status == "" {
			t.Errorf("row %d: dlg=%d status=%q", i, n.dlg, n.status)
		}
	}
	// A missing config.
	m = loadedFrom(filepath.Join(t.TempDir(), "none"), 100, 30)
	if m = keyRune(m, 'm'); m.dlg != dlgNone || !contains(m.status, "does not exist") {
		t.Errorf("missing config: dlg=%d status=%q", m.dlg, m.status)
	}
	// A config that was not read to its end has no hash.
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n# "+strings.Repeat("x", 1<<20)+"\n"), 0o600)
	m = loadedFrom(f, 100, 30)
	if m = keyRune(m, 'm'); m.dlg != dlgNone || !contains(m.status, "not fully read") {
		t.Errorf("truncated config: dlg=%d status=%q", m.dlg, m.status)
	}
}

// run executes cmd and gives its message to the model. These tests never run
// a tick command: it would start ssh.
func run(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("want a command")
	}
	n, next := m.Update(cmd())
	return n.(Model), next
}

func TestMoveFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	const before = "# sshwave: group=Work\nHost web\n  User u\n\nHost db db2\n  User d\n\nHost *\n  User x\n"
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte(before), 0o600)

	m := selectAlias(t, loadedFrom(f, 100, 30), "db2")
	m.consent = true
	m.eff["db2"] = &effState{}
	seq := m.seq

	// The dialog lists the groups, Ungrouped, and the name input.
	m = keyRune(m, 'm')
	if m.dlg != dlgMove || len(m.mv.items) != 3 || m.mv.cur != 1 {
		t.Fatalf("dialog: dlg=%d %+v", m.dlg, m.mv)
	}
	if out := ansi.Strip(m.render()); !contains(out, "Move to group") || !contains(out, "Ungrouped  (current)") || !contains(out, "New group…") {
		t.Fatalf("dialog text:\n%s", out)
	}
	// q and esc close it without a write.
	if n := keyRune(m, 'q'); n.dlg != dlgNone || n.editing {
		t.Error("q must close the dialog")
	}
	// Enter on the current group writes nothing.
	n, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || n.(Model).editing || !contains(n.(Model).status, "already in Ungrouped") {
		t.Errorf("same group: cmd=%v status=%q", cmd != nil, n.(Model).status)
	}

	// Up, then enter: move to Work.
	m = press(m, tea.KeyUp)
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	if m.dlg != dlgNone || !m.editing || m.consent || len(m.eff) != 0 || m.seq == seq {
		t.Fatalf("confirm must end the consent: editing=%v consent=%v eff=%d", m.editing, m.consent, len(m.eff))
	}
	if again := keyRune(m, 'm'); again.dlg != dlgNone {
		t.Error("m opened a dialog during a write")
	}
	if x := keyRune(keyRune(m, 'x'), 'y'); x.consent {
		t.Error("consent came back during a write")
	}
	m, cmd = run(t, m, cmd) // the write
	const after = "# sshwave: group=Work\nHost web\n  User u\n\n# sshwave: group=Work\nHost db db2\n  User d\n\nHost *\n  User x\n"
	if got, _ := os.ReadFile(f); string(got) != after {
		t.Fatalf("file = %q", got)
	}
	baks, _ := os.ReadDir(filepath.Join(xdg, "sshwave", "backups"))
	if len(baks) != 1 {
		t.Fatalf("%d backups", len(baks))
	}
	if b, _ := os.ReadFile(filepath.Join(xdg, "sshwave", "backups", baks[0].Name())); string(b) != before {
		t.Errorf("backup = %q", b)
	}
	if want := "moved db2 to Work · backup: " + baks[0].Name(); m.status != want || m.statusKind != stOK {
		t.Errorf("status = %q, want %q", m.status, want)
	}
	if !m.editing {
		t.Error("the edit state must last until the reload")
	}
	m, cmd = run(t, m, cmd) // the reload
	if cmd != nil || m.editing || m.consent || m.selectedAlias() != "db2" || !contains(m.status, "moved db2 to Work") {
		t.Errorf("after reload: cmd=%v editing=%v consent=%v alias=%q status=%q", cmd != nil, m.editing, m.consent, m.selectedAlias(), m.status)
	}
	if got := labels(m); got != "[Work] web db db2 *" {
		t.Errorf("rows = %s", got)
	}

	// A new group: the name input refuses a bad name and shows why.
	m = keyRune(m, 'm')
	m = press(press(m, tea.KeyDown), tea.KeyDown) // Work -> Ungrouped -> New group
	m = press(m, tea.KeyEnter)
	if !m.mv.typing {
		t.Fatalf("name input not open: %+v", m.mv)
	}
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // empty name
	m = n.(Model)
	if cmd != nil || m.dlg != dlgMove || m.mv.err == "" {
		t.Fatalf("empty name accepted: %+v", m.mv)
	}
	m = typeText(m, strings.Repeat("x", 65))
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	if cmd != nil || m.dlg != dlgMove || !contains(ansi.Strip(m.render()), "longer than 64 bytes") {
		t.Fatalf("long name accepted or no reason shown:\n%s", ansi.Strip(m.render()))
	}
	for range 65 {
		m = press(m, tea.KeyBackspace)
	}
	if m.mv.name != "" || m.mv.err != "" {
		t.Fatalf("backspace: %+v", m.mv)
	}
	// A pasted control character cannot reach the file.
	m = typeText(m, "a")
	n, _ = m.Update(tea.KeyPressMsg{Text: "\nProxyCommand evil"})
	n, cmd = n.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || n.(Model).mv.err == "" {
		t.Fatal("a name with a newline was accepted")
	}
	if got, _ := os.ReadFile(f); string(got) != after {
		t.Fatalf("file changed: %q", got)
	}
	m = press(m, tea.KeyBackspace)
	// Letters that are keys elsewhere are text here. Outer spaces are cut.
	m = typeText(m, " q j? k ")
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = n.(Model)
	m, cmd = run(t, m, cmd)
	m, _ = run(t, m, cmd)
	want := strings.Replace(after, "# sshwave: group=Work\nHost db", "# sshwave: group=q j? k\nHost db", 1)
	if got, _ := os.ReadFile(f); string(got) != want {
		t.Fatalf("file = %q", got)
	}
	if got := labels(m); got != "[Work] web [q j? k] db db2 *" || m.selectedAlias() != "db2" {
		t.Errorf("rows = %s, alias %q", got, m.selectedAlias())
	}

	// Move to Ungrouped. The target group is folded: the move unfolds it.
	m.folded[""] = true
	m = selectAlias(t, m, "db")
	m = press(keyRune(m, 'm'), tea.KeyDown) // q j? k -> Ungrouped
	n, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, cmd = run(t, n.(Model), cmd)
	m, _ = run(t, m, cmd)
	if got, _ := os.ReadFile(f); string(got) != before {
		t.Fatalf("file = %q, want the first content", got)
	}
	if got := labels(m); got != "[Work] web [Ungrouped] db db2 *" || m.selectedAlias() != "db" || !contains(m.status, "moved db to Ungrouped") {
		t.Errorf("rows = %s, alias %q, status %q", got, m.selectedAlias(), m.status)
	}
}

func TestMoveStaleFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("# sshwave: group=Work\nHost web\nHost db\n"), 0o600)
	m := keyRune(selectAlias(t, loadedFrom(f, 100, 30), "db"), 'm')
	const changed = "# sshwave: group=Work\nHost web\nHost db\n  User other\n"
	os.WriteFile(f, []byte(changed), 0o600) // another program edits the file
	n, cmd := press(m, tea.KeyUp).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	if m = keyRune(m, 'm'); m.dlg != dlgMove {
		t.Error("m must work after the reload")
	}
	// A reload while the dialog is open closes it: its lines are old.
	n, _ = m.Update(loadedMsg{sshconfig.Load(f)})
	if n.(Model).dlg != dlgNone {
		t.Error("a reload must close the move dialog")
	}
}

func TestGroupNamesAreSanitized(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const evil = "g‮gnp⁦‏x"
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("# sshwave: group="+evil+"\nHost a\nHost b\n"), 0o600)
	m := loadedFrom(f, 120, 40)
	if got := m.cfg.Groups(); len(got) != 1 || got[0] != evil {
		t.Fatalf("groups = %q", got)
	}
	views := map[string]Model{"header": m, "host": selectAlias(t, m, "a")}
	d := keyRune(selectAlias(t, m, "b"), 'm')
	views["dialog"] = d
	d = press(press(press(d, tea.KeyDown), tea.KeyDown), tea.KeyEnter)
	n, _ := d.Update(tea.KeyPressMsg{Text: "n\x1b[2J‮\r"})
	n, _ = n.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !n.(Model).mv.typing || n.(Model).mv.err == "" {
		t.Fatal("the name input must stay open with a reason")
	}
	views["input"] = n.(Model)
	m.setStatus(stOK, "moved a to "+evil)
	views["status"] = m
	for name, v := range views {
		if plain := noControl(t, name, v.render()); !contains(plain, "g·gnp··x") && name != "input" {
			t.Errorf("%s: the group name is not visible as text:\n%s", name, plain)
		}
	}
}
