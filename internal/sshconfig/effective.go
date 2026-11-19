//go:build unix

package sshconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Pair is one key/value from `ssh -G`.
type Pair struct{ Key, Value string }

// SafeAlias reports whether alias can be passed to ssh as a host argument.
func SafeAlias(alias string) error {
	if alias == "" {
		return errors.New("empty alias")
	}
	if strings.HasPrefix(alias, "-") {
		return errors.New("alias starts with '-'")
	}
	for _, r := range alias {
		if r <= ' ' || r == 0x7f {
			return errors.New("alias has white space or control characters")
		}
	}
	return nil
}

// effTimeout is the longest time one `ssh -G` call can run.
var effTimeout = 5 * time.Second

// maxEffBytes is the most that the app keeps of the standard output of one
// `ssh -G` call, and of its standard error.
const maxEffBytes = 1 << 20

// Args returns the arguments of ssh for alias: `ssh -G` with effective, a
// connection without. The default ~/.ssh/config gets no -F, so ssh reads the
// system config and the user config as usual. Each other file gets -F, and
// ssh then ignores the system config. -- ends the options.
func Args(configPath string, effective bool, alias string) []string {
	var args []string
	if !IsDefault(configPath) {
		args = append(args, "-F", configPath)
	}
	if effective {
		args = append(args, "-G")
	}
	return append(args, "--", alias)
}

// capWriter keeps the first max bytes. A write that goes above max sets over
// and calls stop, and the writer drops all later bytes.
type capWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	max  int
	over bool
	stop func()
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.over && w.buf.Len()+len(p) > w.max {
		w.over = true
		w.stop()
	}
	if !w.over {
		w.buf.Write(p)
	}
	return len(p), nil // no error: ssh must not block on a full pipe
}

// result returns the kept bytes and whether the output went above max.
func (w *capWriter) result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String(), w.over
}

// Effective asks OpenSSH for the final settings of alias. OpenSSH stays the
// authority. Callers must get consent first, because `ssh -G` can run Match
// exec commands. ssh runs in its own process group. At the timeout, or when
// the output is too large, the whole group is killed, so Match exec children
// stop too.
func Effective(configPath, alias string) ([]Pair, error) {
	if err := SafeAlias(alias); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), effTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", Args(configPath, true, alias)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second // do not wait for a child that keeps the pipes open
	out := &capWriter{max: maxEffBytes, stop: cancel}
	errb := &capWriter{max: maxEffBytes, stop: cancel}
	cmd.Stdout, cmd.Stderr = out, errb
	err := cmd.Run()
	stdout, over1 := out.result()
	stderr, over2 := errb.result()
	if over1 || over2 {
		return nil, errors.New("ssh -G failed: output is more than 1 MiB")
	}
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if ctx.Err() != nil {
			msg = fmt.Sprintf("timed out after %s", effTimeout)
		} else if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ssh -G failed: %s", msg)
	}
	var pairs []Pair
	for _, line := range strings.Split(stdout, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			pairs = append(pairs, Pair{Key: k, Value: strings.TrimSpace(v)})
		}
	}
	return pairs, nil
}
