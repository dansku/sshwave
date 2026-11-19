//go:build unix

// Package backup stores exact copies of config files before they change.
package backup

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/dansku/sshwave/internal/safefile"
)

// Keep is the number of backups kept for each source file.
const Keep = 50

// MaxSize is the largest file that Create accepts.
const MaxSize = 16 << 20

// maxBase keeps the backup name below NAME_MAX (255 bytes).
const maxBase = 150

// Dir returns the backup folder: $XDG_DATA_HOME/sshwave/backups, or
// ~/.local/share/sshwave/backups. A relative XDG_DATA_HOME is ignored, as
// the XDG specification says.
func Dir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(base) {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, ".local", "share")
	}
	return filepath.Join(base, "sshwave", "backups"), nil
}

// secureDir creates the backup folder. The sshwave and backups folders must
// be real directories that the current user owns. A looser mode becomes 0700.
func secureDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(dir)), 0o700); err != nil {
		return err
	}
	// One level at a time, so nothing is made through a symlink.
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.Mkdir(d, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		st, err := os.Lstat(d)
		if err != nil {
			return err
		}
		if !st.IsDir() { // Lstat does not follow, so a symlink fails here
			return fmt.Errorf("%s is not a real directory", d)
		}
		if sys, ok := st.Sys().(*syscall.Stat_t); !ok || int(sys.Uid) != os.Getuid() {
			return fmt.Errorf("%s is not owned by the current user", d)
		}
		if st.Mode().Perm()&0o077 != 0 {
			if err := os.Chmod(d, 0o700); err != nil {
				return fmt.Errorf("%s has loose mode %04o: %w", d, st.Mode().Perm(), err)
			}
		}
	}
	return nil
}

// suffixFor returns the name ending that identifies one source path:
// -<base name>-<first 8 hex digits of sha256(path)>.bak
func suffixFor(src string) string {
	base := filepath.Base(src)
	for len(base) > maxBase { // cut whole runes; some file systems refuse invalid UTF-8
		_, n := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-n]
	}
	sum := sha256.Sum256([]byte(src))
	return fmt.Sprintf("-%s-%x.bak", base, sum[:4])
}

// Create reads src and calls CreateFromBytes.
func Create(src string) (string, error) {
	data, err := safefile.ReadAll(src, MaxSize)
	if err != nil {
		return "", err
	}
	return CreateFromBytes(src, data)
}

// CreateFromBytes writes data, the content of src, into the backup folder and
// checks the copy. It returns the backup path. It removes no older backup:
// the caller calls Prune when the new backup stays.
func CreateFromBytes(src string, data []byte) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := secureDir(dir); err != nil {
		return "", err
	}
	suffix := suffixFor(src)
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + suffix
	dst := filepath.Join(dir, name)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	for _, e := range []error{werr, serr, cerr} {
		if e != nil {
			os.Remove(dst)
			return "", e
		}
	}
	back, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(back, data) {
		os.Remove(dst)
		return "", fmt.Errorf("backup verification failed for %s", src)
	}
	return dst, nil
}

// Prune removes the oldest backups of src until Keep are left. Call it only
// after the edit of src is done and its new backup stays. Then an edit that
// fails or changes nothing costs no older backup.
func Prune(src string) {
	dir, err := Dir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	suffix := suffixFor(src)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // timestamp prefix sorts oldest first
	for len(names) > Keep {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}
