<p align="center"><img src="docs/assets/sshwave-logo.png" alt="sshwave" width="720"></p>

<p align="center"><b>Browse your <code>~/.ssh/config</code> in a synthwave terminal UI, and edit it without fear.</b></p>

<p align="center">
  <a href="https://github.com/dansku/sshwave/actions/workflows/ci.yml"><img src="https://github.com/dansku/sshwave/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/dansku/sshwave/releases/latest"><img src="https://img.shields.io/github/v/release/dansku/sshwave" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/dansku/sshwave" alt="License: MIT"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/dansku/sshwave" alt="Go version">
  <a href="https://github.com/charmbracelet/bubbletea"><img src="https://img.shields.io/badge/built%20with-Bubble%20Tea-ff2e97" alt="Built with Bubble Tea"></a>
</p>

<p align="center"><img src="docs/assets/screenshot.svg" alt="The sshwave terminal UI: a host list with Groups and Notes on the left, and the details of one host on the right" width="900"></p>

The picture is the real UI, drawn from the fake data in `testdata/ssh-notes`. `make screenshot` redraws it, and `make demo` records a GIF with [VHS](https://github.com/charmbracelet/vhs).

## Why

- **Your config stays the only store.** Hosts, Groups, and Notes all live in `~/.ssh/config`. There is no database and no import step.
- **Nothing is rewritten.** The app does not reformat or reorder your file. A Group or Note change touches only its own comment lines.
- **A backup before each edit.** The app makes an exact, private copy first. If the backup fails, the edit does not start.
- **Consent before `ssh -G`.** Effective settings come from `ssh -G`, which can run `Match exec` commands. The app asks first, each time the file is loaded.

## Features

- A host list from your config and its `Include` files, with wildcard and `Match` blocks shown apart as rules.
- Fuzzy search in aliases, host names, users, group names, and note text.
- A detail panel with the block as written, and the effective settings that OpenSSH computes.
- Groups that you can fold, and multi-line Notes, both stored as comments that OpenSSH ignores.
- Edit in your own editor (`$VISUAL`, `$EDITOR`, or `vi`). Common terminal editors open at the line of the selected host.
- Connect to the selected host with your own `ssh`.
- Limits on file size, file count, and `Include` depth, and no terminal escape sequences from file content.

## Install

You need macOS or Linux, and the OpenSSH client (`ssh`) in your `PATH`.

**With Go** (the version in [go.mod](go.mod) or newer):

```sh
go install github.com/dansku/sshwave/cmd/sshwave@latest
```

**From a release archive** (from the first tagged release on). Each [release](https://github.com/dansku/sshwave/releases) has `sshwave_<os>_<arch>.tar.gz` for `darwin` and `linux`, `amd64` and `arm64`, and a `SHA256SUMS` file:

```sh
curl -LO https://github.com/dansku/sshwave/releases/latest/download/sshwave_linux_amd64.tar.gz
curl -LO https://github.com/dansku/sshwave/releases/latest/download/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS   # macOS: shasum -a 256 --check --ignore-missing SHA256SUMS
tar -xzf sshwave_linux_amd64.tar.gz
sudo install -m 755 sshwave_linux_amd64/sshwave /usr/local/bin/
```

**From source:**

```sh
git clone https://github.com/dansku/sshwave
cd sshwave
make build      # ./sshwave
make install    # or: install to GOBIN
```

## Quick start

```sh
sshwave                 # opens ~/.ssh/config
sshwave -config path    # opens another file
sshwave -version
```

Move with `j`/`k`, search with `/`, and press `?` for the keys. To try it without your own config, run `make run CONFIG=testdata/ssh-notes/config` in a clone.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, `↓` / `↑` | Move |
| `g` / `G`, `Home` / `End` | Top / bottom |
| `PgDn` / `PgUp`, `Ctrl+D` / `Ctrl+U` | Half a page down / up |
| `/` | Search. `Enter` keeps the filter, `Esc` clears it |
| `Enter`, `e` | Back up the config, then open it in your editor |
| `Enter`, `Space` on a group header | Fold or unfold the group |
| `m` | Move the selected host to a Group |
| `n` | Edit the Note of the selected host (`Ctrl+S` saves, `Esc` cancels) |
| `c` | Connect to the selected host with `ssh` |
| `x`, then `y` | Allow `ssh -G` for this load, to show effective settings |
| `?` | Help |
| `q`, `Ctrl+C` | Quit |

## Groups and Notes

A Group is a label on a `Host` block. A Note is free text on it. The app stores both as tag comments directly above the `Host` line:

```
# sshwave: group=Work
# sshwave: note=Primary API box.
# sshwave: note=Deploys run at 02:00 UTC.
Host production-api prod
    HostName 192.168.1.50
    User ubuntu
```

- All aliases of a block share its Group and its Note. A block has one Group at most.
- A Group name has 64 bytes at most. A Note has 10 lines of 200 bytes at most.
- When a host has a Group, the list shows a header for each Group, in file order. Hosts without one come last, below `Ungrouped`.
- The list shows `✎` after a host that has a Note.
- A Note is plain text in your config. Do not store a password in it.
- The app writes tags only in your main config, and only for hosts with a literal name. It shows the tags of `Include` files but does not change them.

[docs/tags.md](docs/tags.md) has the exact format, so other tools can read and write it.

## Safety

- **Read.** Reading changes no file and runs no program. It has limits on files, bytes, `Include` patterns, glob matches, and recorded problems, so a broken or hostile config cannot hang the app or fill its memory. When a limit is reached, or when `ssh` can read other files than the app (an `Include` with a variable, with `~user`, with a bad pattern, or a relative `Include` in a file that is not `~/.ssh/config`), the warning before `ssh -G` is stronger.
- **Back up.** Before each edit the app saves an exact copy to `~/.local/share/sshwave/backups/` (or `$XDG_DATA_HOME/sshwave/backups/`), mode `0600`. It keeps the newest 50, and deletes older ones only after an edit that changed the file.
- **Write.** A Group or Note change checks that the file did not change on disk, makes the backup, writes a temporary file, and renames it over the config.
- **Run.** The app runs `ssh -G` only after you allow it with `x`, then `y`. No other key accepts. That permission ends at each edit and each reload. It also ends when a file that the app read has changed on disk: the app checks this before each `ssh -G` run, and then loads the config again. The app keeps 1 MiB at most of the output of `ssh -G`. `c` is different: it starts a real `ssh` connection at once, and `ssh` then applies your whole config. For `~/.ssh/config` the app passes no `-F`, so `ssh` reads `/etc/ssh/ssh_config` and your config as usual. For a file from `-config` it passes `-F <file>`, and `ssh` then ignores `/etc/ssh/ssh_config`.
- **Show.** Control and bidirectional characters from files, `ssh -G`, and errors become `·` on screen.

[docs/safety.md](docs/safety.md) has the full model, the limits, the residual risks, and how to restore a backup by hand. To report a vulnerability, see [SECURITY.md](SECURITY.md).

## How it compares

sshwave is a viewer and a careful editor for a file that you already have. It is not an SSH client: `c` only starts your own `ssh`. It does not manage keys, store passwords, or keep a host list of its own. If you stop using it, your config still works as before; the tags are plain comments.

## Platform support

macOS and Linux, on `amd64` and `arm64`. Windows is not supported: the code uses Unix calls, and a Windows build stops with a build-constraint error.

## Contributing

Issues and pull requests are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md): it covers `make check`, the golden views, and the fake-data rule for fixtures. Please follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

[MIT](LICENSE)

## Acknowledgements

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and other [Charm](https://charm.sh) libraries. The look of the UI was inspired by [Crush](https://github.com/charmbracelet/crush); sshwave shares no code with it.
