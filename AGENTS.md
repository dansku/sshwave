# sshwave

A synthwave Bubble Tea v2 terminal app for macOS and Linux. It browses the hosts in `~/.ssh/config` and its `Include` files, shows effective settings from `ssh -G` after consent, opens the file in the user's editor after a backup, stores host Groups and Notes as tag comments, and starts `ssh` for the selected host. Module path: `github.com/dansku/sshwave`.

## Layout

- `cmd/sshwave/` — `main`: flags (`-config`, `-version`), then the UI.
- `internal/sshconfig/` — read-only parser, tag parsing (`group.go`, `note.go`), fuzzy score, `ssh -G` runner (`effective.go`).
- `internal/sshedit/` — the only writer of the config: tag edits with hash check, backup, temporary file, rename.
- `internal/backup/` — backup folder, names, modes, pruning.
- `internal/edit/` — editor session: backup, editor command, cleanup.
- `internal/safefile/` — bounded, non-blocking read of regular files.
- `internal/ui/` — Bubble Tea model, view, dialogs, theme, `sanitize`. Golden views in `internal/ui/testdata/`.
- `testdata/` — fixture configs with fake data.
- `docs/safety.md`, `docs/tags.md` — the safety model and the tag grammar. `GLOSSARY.md` — the project words.

## Commands

- `make check` — format, vet, test, verify modules. Run it before you finish.
- `go test -race -count=1 ./...` — what CI runs.
- `make golden` — refresh golden views. Run it only for an intended view change, and review each diff.
- `make build`, `make dist`, `make cover`, `make fmt-check`, `make demo`. `make help` lists all.

## Rules

- Do not weaken the safe-write flow or the consent gate. `sshedit.apply` is the only path that writes the config. `ssh -G` runs only when `Model.effAllowed` is true. Keep `docs/safety.md` and `docs/tags.md` exact when limits or steps change.
- Start no shell. Use `exec.Command` with an argument list and `--` before the alias. Build the `ssh` arguments with `sshconfig.Args`: it passes `-F` only when the Main config is not `~/.ssh/config`.
- Pass each string from a file, from `ssh`, or from an error through `sanitize` before it reaches the screen.
- Tests must not read the real `~/.ssh` or write to the real backup folder. Set `HOME` and `XDG_DATA_HOME` to temporary folders.
- Fixtures, tests, golden views, and docs use fake data only.
- Use the words in `GLOSSARY.md`.
- Unix only. Files with Unix calls have `//go:build unix`. Do not add Windows code paths.
- Keep it simple. Add no dependency without a clear benefit.
- Do not hard-wrap Markdown prose.
