package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Only y gives Consent. Enter is easy to press by accident.
func TestConsentNeedsY(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host a\n"), 0o600)
	m := keyRune(loadedFrom(f, 100, 30), 'x')
	if m.dlg != dlgConsent {
		t.Fatal("x must open the consent dialog")
	}
	n, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := n.(Model); got.consent || got.effAllowed() || cmd != nil || got.dlg != dlgConsent {
		t.Errorf("enter: consent=%v cmd=%v dlg=%d", got.consent, cmd != nil, got.dlg)
	}
	if got := keyRune(m, 'y'); !got.consent || got.dlg != dlgNone {
		t.Errorf("y: consent=%v dlg=%d", got.consent, got.dlg)
	}
}

// consented returns a model with Consent for a config that includes one
// file, and a fake ssh that writes the file marker when it runs.
func consented(t *testing.T) (m Model, inc, marker string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	bin, d := t.TempDir(), t.TempDir()
	marker = filepath.Join(d, "ssh-ran")
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\necho ran > "+marker+"\necho 'hostname a.example'\n"), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := filepath.Join(d, "config")
	inc = filepath.Join(d, "inc.conf")
	os.WriteFile(f, []byte("Include "+d+"/*.conf\nHost a\n"), 0o600)
	os.WriteFile(inc, []byte("Host b\n"), 0o600)
	m = keyRune(keyRune(loadedFrom(f, 100, 30), 'x'), 'y')
	if !m.effAllowed() || m.selectedAlias() != "b" {
		t.Fatalf("setup: allowed=%v alias=%q", m.effAllowed(), m.selectedAlias())
	}
	return m, inc, marker
}

func TestEffectiveRunsForUnchangedFiles(t *testing.T) {
	m, _, marker := consented(t)
	n, cmd := m.Update(effTickMsg{m.seq, "b"})
	if cmd == nil {
		t.Fatal("the tick must start ssh -G")
	}
	n, _ = n.(Model).Update(cmd())
	m = n.(Model)
	if _, err := os.Stat(marker); err != nil {
		t.Error("ssh did not run for unchanged files")
	}
	if st := m.eff["b"]; !m.consent || st == nil || st.err != nil || len(st.pairs) != 1 {
		t.Errorf("consent=%v eff=%+v", m.consent, st)
	}
}

// Consent is for the bytes that the app read. When a file that was read has
// other bytes now, or an Include matches a new file, ssh -G must not run.
func TestConsentEndsWhenAFileChanges(t *testing.T) {
	for name, change := range map[string]func(inc string){
		"main config": func(inc string) {
			os.WriteFile(filepath.Join(filepath.Dir(inc), "config"), []byte("Match exec \"x\"\n"), 0o600)
		},
		"included file": func(inc string) { os.WriteFile(inc, []byte("Host b\nMatch exec \"x\"\n"), 0o600) },
		"new match": func(inc string) {
			os.WriteFile(filepath.Join(filepath.Dir(inc), "new.conf"), []byte("Match exec \"x\"\n"), 0o600)
		},
		"removed file": func(inc string) { os.Remove(inc) },
	} {
		t.Run(name, func(t *testing.T) {
			m, inc, marker := consented(t)
			n, cmd := m.Update(effTickMsg{m.seq, "b"})
			if cmd == nil {
				t.Fatal("the tick must start the check")
			}
			change(inc)
			msg := cmd()
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("ssh ran after a file changed")
			}
			n, reload := n.(Model).Update(msg)
			m = n.(Model)
			if m.consent || m.effAllowed() || len(m.eff) != 0 || reload == nil || !contains(m.status, "changed on disk") {
				t.Errorf("consent=%v eff=%d reload=%v status=%q", m.consent, len(m.eff), reload != nil, m.status)
			}
			if _, ok := reload().(loadedMsg); !ok {
				t.Error("the app must load the config again")
			}
		})
	}
}

// A late answer from before an edit or a reload must not end a newer Consent.
func TestOldStaleAnswerIsDropped(t *testing.T) {
	m, _, _ := consented(t)
	n, cmd := m.Update(effStaleMsg{st: &effState{}, alias: "b"})
	if got := n.(Model); !got.consent || cmd != nil || got.status != "" {
		t.Errorf("consent=%v cmd=%v status=%q", got.consent, cmd != nil, got.status)
	}
}

// c starts ssh without -F for ~/.ssh/config, and with -F for another file.
func TestConnectArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	def := filepath.Join(home, ".ssh", "config")
	other := filepath.Join(t.TempDir(), "config")
	for path, want := range map[string][]string{
		def:   {"ssh", "--", "a"},
		other: {"ssh", "-F", other, "--", "a"},
	} {
		os.WriteFile(path, []byte("Host a\n"), 0o600)
		m := loadedFrom(path, 100, 30)
		c, err := m.connectCommand()
		if err != nil || !slices.Equal(c.Args, want) {
			t.Errorf("%s: args %q err %v, want %q", path, c.Args, err, want)
		}
	}
	os.WriteFile(def, []byte("Host -oProxyCommand=x\n"), 0o600)
	if _, err := loadedFrom(def, 100, 30).connectCommand(); err == nil {
		t.Error("an unsafe alias must give an error")
	}
}

// The header must not show the entry "more problems not shown" as a problem.
func TestHeaderProblemCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte(strings.Repeat("Include $\n", 500)), 0o600)
	if out := ansi.Strip(loadedFrom(f, 100, 30).header()); !contains(out, "100+ problems") {
		t.Errorf("header = %q", out)
	}
}
