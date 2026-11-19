package ui

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dansku/sshwave/internal/sshconfig"
)

var update = flag.Bool("update", false, "update golden files")

// TestMain gives the tests an empty home and data folder, so no test reads
// the real ~/.ssh or writes a backup outside a temporary folder.
func TestMain(m *testing.M) {
	home, _ := os.MkdirTemp("", "home")
	os.Setenv("HOME", home)
	os.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	noteFixture = copyFixture(home, "../../testdata/ssh-notes")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func loaded(w, h int) Model {
	m := New("../../testdata/ssh/config")
	m.w, m.h = w, h
	next, _ := m.Update(loadedMsg{sshconfig.Load(m.path)})
	return next.(Model)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(p, []byte(got), 0o644)
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", p, err)
	}
	if string(want) != got {
		t.Errorf("%s differs\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestGoldenList(t *testing.T) {
	for _, s := range []struct {
		name string
		w, h int
	}{{"list_80x24", 80, 24}, {"list_120x40", 120, 40}, {"list_60x16", 60, 16}} {
		t.Run(s.name, func(t *testing.T) {
			golden(t, s.name, ansi.Strip(loaded(s.w, s.h).render()))
		})
	}
}

func TestGoldenHelpAndEmpty(t *testing.T) {
	m := loaded(80, 24)
	m.dlg = dlgHelp
	golden(t, "help_80x24", ansi.Strip(m.render()))

	e := New(filepath.Join(t.TempDir(), "none"))
	e.w, e.h = 80, 24
	n, _ := e.Update(loadedMsg{sshconfig.Load(e.path)})
	// path differs per run, so check content instead of a golden file
	out := ansi.Strip(n.(Model).render())
	if !contains(out, "No SSH config found") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFilterAndIncludedEdit(t *testing.T) {
	m := loaded(100, 30)
	for _, r := range "pi" {
		if m.query == "" {
			m.filtering = true
		}
		n, _ := m.key(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = n.(Model)
	}
	if len(m.rows) == 0 || len(m.rows) >= len(m.cfg.Hosts)+len(m.cfg.Rules) {
		t.Errorf("filter should narrow the rows, got %d", len(m.rows))
	}
	// included host: edit must be refused without starting an editor
	m = loaded(100, 30) // cursor 0 = github (included)
	if cmd, starts := m.startEdit(); cmd == nil || starts {
		t.Fatal("want a command")
	} else if pm, ok := cmd().(prepMsg); !ok || pm.err == nil || pm.sess != nil {
		t.Errorf("included host edit must fail with a notice: %+v", pm)
	}
}

func keyRune(m Model, r rune) Model {
	n, _ := m.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	return n.(Model)
}

// These tests never run a command from an effTickMsg: it would start ssh.
func TestConsentIsNeededForEachLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n  HostName a.example\n"), 0o600) // no Match exec
	m := New(f)
	n, cmd := m.Update(loadedMsg{sshconfig.Load(f)})
	m = n.(Model)
	if cmd != nil || m.effAllowed() {
		t.Fatal("ssh -G scheduled without consent")
	}
	if n, cmd := m.Update(effTickMsg{m.seq, "a"}); cmd != nil || len(n.(Model).eff) != 0 {
		t.Fatal("effTickMsg ran without consent")
	}

	m = keyRune(m, 'x')
	if m.dlg != dlgConsent {
		t.Fatal("x must open the consent dialog")
	}
	n, cmd = m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = n.(Model)
	if !m.consent || cmd == nil {
		t.Fatal("y must allow and schedule ssh -G")
	}
	tick := effTickMsg{m.seq, "a"}
	n, cmd = m.Update(tick)
	if cmd == nil {
		t.Fatal("tick with consent must start ssh -G")
	}
	running := n.(Model)
	st := running.eff["a"]

	// An edit start removes the consent, the cache, and queued ticks.
	n, cmd = running.key(tea.KeyPressMsg{Code: 'e', Text: "e"})
	m = n.(Model)
	if cmd == nil || m.consent || len(m.eff) != 0 {
		t.Fatalf("edit start: consent=%v eff=%d", m.consent, len(m.eff))
	}
	if _, cmd := m.Update(tick); cmd != nil {
		t.Error("queued effTickMsg ran after the edit start")
	}
	if n, _ := m.Update(effMsg{st: st, alias: "a"}); len(n.(Model).eff) != 0 {
		t.Error("old ssh -G result was stored after the edit start")
	}

	// Until the reload, x cannot give the consent back (type-ahead "exy").
	m = keyRune(keyRune(m, 'x'), 'y')
	if m.dlg != dlgNone || m.consent || m.effAllowed() {
		t.Errorf("consent came back during an edit: dlg=%d consent=%v", m.dlg, m.consent)
	}
	if _, cmd := m.Update(effTickMsg{m.seq, "a"}); cmd != nil {
		t.Error("effTickMsg ran during an edit")
	}
	if n, _ := m.Update(prepMsg{err: errors.New("no backup")}); n.(Model).editing {
		t.Error("a failed edit start must end the edit state")
	}

	// A reload does the same, and closes an open consent dialog.
	m.consent, m.dlg = true, dlgConsent
	m.eff["a"] = &effState{}
	seq := m.seq
	n, cmd = m.Update(loadedMsg{sshconfig.Load(f)})
	m = n.(Model)
	if cmd != nil || m.consent || m.editing || len(m.eff) != 0 || m.seq == seq || m.dlg != dlgNone {
		t.Errorf("reload: consent=%v eff=%d seq=%d dlg=%d", m.consent, len(m.eff), m.seq, m.dlg)
	}
}

// A second edit start during an edit must do nothing. Before, a refused start
// (a missing file, an Included host) ended the edit state of a write that was
// still on its way, and x could then give consent before the reload.
func TestNoEditStartDuringAnEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := loaded(80, 24) // the cursor is on github, a host from an Included file
	if r, ok := m.current(); !ok || r.host == nil || !r.host.Block.Included {
		t.Fatal("the fixture must start on an Included host")
	}
	if _, cmd := m.key(tea.KeyPressMsg{Code: 'e', Text: "e"}); cmd == nil {
		t.Fatal("without an edit, e on an Included host must report why not")
	}
	m.editing = true // a tag write or an editor start is on its way
	for _, k := range []tea.KeyPressMsg{{Code: 'e', Text: "e"}, {Code: tea.KeyEnter}} {
		n, cmd := m.key(k)
		if cmd != nil || !n.(Model).editing {
			t.Errorf("%q started a second edit: cmd=%v editing=%v", k.String(), cmd != nil, n.(Model).editing)
		}
	}
	if m = keyRune(keyRune(m, 'x'), 'y'); m.consent || m.dlg != dlgNone {
		t.Error("consent came back during an edit")
	}
}

func TestConsentWording(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for text, want := range map[string]string{
		"Host a\n":                    "not resolved yet",
		"Host a\nMatch !exec true\n":  "has Match exec",
		"Host a\nInclude ${X}/conf\n": "not fully read",
	} {
		f := filepath.Join(t.TempDir(), "config")
		os.WriteFile(f, []byte(text), 0o600)
		m := New(f)
		n, _ := m.Update(loadedMsg{sshconfig.Load(f)})
		m = n.(Model)
		if out := ansi.Strip(m.render()); !contains(out, want) || !contains(out, "resolve") {
			t.Errorf("%q: panel or footer lacks the hint %q:\n%s", text, want, out)
		}
		m = keyRune(m, 'x')
		if out := ansi.Strip(m.render()); !contains(out, want) || !contains(out, "Run ssh -G?") {
			t.Errorf("%q: dialog lacks %q:\n%s", text, want, out)
		}
	}
}

func TestSanitize(t *testing.T) {
	in := "a\x1b[31mb\x07c\rd\te\x7ff\u009bg\u202eh\u2066i\u200fj\u2028k"
	if got, want := sanitize(in), "a·[31mb·c·d·e·f·g·h·i·j·k"; got != want {
		t.Errorf("sanitize = %q, want %q", got, want)
	}
}

func TestRenderHasNoControlCharacters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const evil = "\x1b]8;;http://evil\x07link\x1b]8;;\x07\x1bPdcs\x1b\\\x1b[2J\u009b2J\u202egnp\u2066\u200f\x7f"
	d := filepath.Join(t.TempDir(), "dir\x1b[2J\u202e")
	os.Mkdir(d, 0o700)
	text := "Host ali" + evil + "\n" +
		"\tHostName name" + evil + "\n" +
		"  # note\rover" + evil + "\n" +
		"  User u\rroot\n" +
		"Host *.rule" + evil + "\n" +
		"Match host m" + evil + "\n"
	f := filepath.Join(d, "config")
	os.WriteFile(f, []byte(text), 0o600)

	base := New(f)
	base.w, base.h = 120, 40
	n, _ := base.Update(loadedMsg{sshconfig.Load(f)})
	base = n.(Model)
	alias := base.cfg.Hosts[0].Alias

	views := map[string]Model{}
	views["consent needed"] = base
	m := base
	m.consent = true
	m.eff[alias] = &effState{pairs: []sshconfig.Pair{{Key: "hostname", Value: "eff" + evil}, {Key: "user", Value: "u\r\n"}}}
	m.query, m.filtering = "q"+evil, true
	views["effective values"] = m
	m = base
	m.consent = true
	m.eff = map[string]*effState{alias: {err: errors.New("ssh -G failed: " + evil)}}
	m.setStatus(stErr, "status"+evil)
	views["errors"] = m
	for i := 1; i < len(base.rows); i++ { // each rule in the detail panel
		m = base
		m.cursor = i
		views[fmt.Sprint("row ", i)] = m
	}
	m = New(filepath.Join(d, "none"))
	m.w, m.h = 120, 40
	n, _ = m.Update(loadedMsg{sshconfig.Load(m.path)})
	views["missing file"] = n.(Model)

	for name, m := range views {
		plain := noControl(t, name, m.render())
		if contains(plain, "2J") && !contains(plain, "·[2J") && !contains(plain, "·2J") {
			t.Errorf("%s: an escape sequence was not made visible", name)
		}
	}
	if plain := ansi.Strip(views["consent needed"].render()); !contains(plain, "·]8;;http://evil·link") {
		t.Errorf("the OSC 8 payload must show as text:\n%s", plain)
	}
}

// noControl fails the test when out has a control or format character that
// is not part of a CSI sequence. It returns the text without the styles.
func noControl(t *testing.T, name, out string) string {
	t.Helper()
	// The raw output: lipgloss emits only CSI sequences (ESC [).
	rs := []rune(out)
	for i, r := range rs {
		switch {
		case r == '\n':
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == '[':
		case r < 0x20 || (r >= 0x7f && r < 0xa0), unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
			t.Fatalf("%s: control character %U in the output", name, r)
		}
	}
	// After Strip only text is left. An escape that came through would be gone.
	plain := ansi.Strip(out)
	for _, r := range plain {
		if r < 0x20 && r != '\n' {
			t.Fatalf("%s: control character %U after Strip", name, r)
		}
	}
	return plain
}

// copyFixture copies a fixture folder to <home>/.ssh and returns the path of
// its config. The app then treats it as the user's default config, as it does
// in real use.
func copyFixture(home, src string) string {
	dst := filepath.Join(home, ".ssh")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	})
	if err != nil {
		panic(err)
	}
	return filepath.Join(dst, "config")
}
