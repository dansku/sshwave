// Package edit runs the user's editor on a config file, with a backup first.
package edit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dansku/sshwave/internal/backup"
	"github.com/dansku/sshwave/internal/safefile"
)

// Session is one edit: a backup made before the editor starts.
type Session struct {
	Path   string // the real file that the editor changes
	Backup string
	before []byte
}

// Prepare resolves path to an absolute real path and backs up that file. The
// absolute path means the editor never gets a name that starts with - or +.
// It reads the file once, from one checked descriptor, so the backup and the
// later comparison use the same bytes. The caller then runs Command and calls
// Finish.
func Prepare(path string) (*Session, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	before, err := safefile.ReadAll(real, backup.MaxSize)
	if err != nil {
		return nil, err
	}
	b, err := backup.CreateFromBytes(real, before)
	if err != nil {
		return nil, fmt.Errorf("backup failed, editor not started: %w", err)
	}
	return &Session{Path: real, Backup: b, before: before}, nil
}

// Finish compares the file with its state before the edit. If nothing changed
// it removes the backup, so the backup folder holds only real changes. If the
// file changed, the backup stays, and only then older backups are pruned.
func (s *Session) Finish() (changed bool, err error) {
	after, err := safefile.ReadAll(s.Path, backup.MaxSize)
	if err != nil {
		return true, err // keep the backup
	}
	if bytes.Equal(after, s.before) {
		os.Remove(s.Backup)
		return false, nil
	}
	backup.Prune(s.Path)
	return true, nil
}

// Command builds the editor command: $VISUAL, then $EDITOR, then vi. It runs
// without a shell. It adds +line for editors that support it.
func Command(file string, line int) (*exec.Cmd, error) {
	spec := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	argv := strings.Fields(spec)
	if len(argv) == 0 {
		return nil, errors.New("no editor configured")
	}
	args := append([]string{}, argv[1:]...)
	if line > 0 && supportsPlusLine(filepath.Base(argv[0])) {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, file)
	return exec.Command(argv[0], args...), nil
}

func supportsPlusLine(name string) bool {
	switch name {
	case "vi", "vim", "nvim", "view", "nano", "pico", "emacs", "micro", "joe":
		return true
	}
	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
