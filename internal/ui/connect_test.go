package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConnectRefusesUnsafeAlias(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config")
	os.WriteFile(cfg, []byte("Host -oProxyCommand=x\n  HostName example.net\n"), 0o600)
	m := loadedFrom(cfg, 100, 30)
	if m.selectedAlias() != "-oProxyCommand=x" {
		t.Fatalf("setup: selected %q", m.selectedAlias())
	}
	n, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd != nil {
		t.Error("an unsafe alias must not start ssh")
	}
	if got := n.(Model).status; !strings.Contains(got, "cannot connect") {
		t.Errorf("status = %q", got)
	}
}

func TestConnectStartsForSafeAlias(t *testing.T) {
	m := loadedFrom("../../testdata/ssh/config", 100, 30)
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"}); cmd == nil {
		t.Error("a safe alias should start ssh")
	}
}
