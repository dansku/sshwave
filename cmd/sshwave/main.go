// Command sshwave is a terminal app to browse and safely edit an OpenSSH
// client config. It runs on macOS and Linux.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/dansku/sshwave/internal/ui"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// buildVersion returns version. A build without the flag, such as one from
// `go install module@version`, reports the module version when Go knows it.
func buildVersion() string {
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
	}
	return version
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without os.Exit, so a test can call it.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sshwave", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("config", "", "SSH config `file` (default ~/.ssh/config)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "sshwave", buildVersion())
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "sshwave: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}
	if *path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(stderr, "sshwave:", err)
			return 1
		}
		*path = filepath.Join(home, ".ssh", "config")
	}
	// An absolute real path never starts with - or +, so ssh and the editor
	// cannot read it as an option. A missing file keeps its absolute name.
	if abs, err := filepath.Abs(*path); err == nil {
		*path = abs
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			*path = real
		}
	}
	if _, err := tea.NewProgram(ui.New(*path)).Run(); err != nil {
		fmt.Fprintln(stderr, "sshwave:", err)
		return 1
	}
	return 0
}
