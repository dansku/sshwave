# Contributing to sshwave

Thank you for your help. This project is small on purpose. Changes that keep it simple and safe are the most welcome.

## Set up

You need Go (the version in `go.mod`), `make`, and macOS or Linux.

```sh
git clone https://github.com/dansku/sshwave
cd sshwave
make check    # format, vet, test, verify modules
make run CONFIG=testdata/ssh-notes/config
```

`make help` lists all targets. The tests use a temporary home and data folder. They do not read your `~/.ssh` and they write no backup outside a temporary folder. Keep it that way in new tests: set `HOME` and `XDG_DATA_HOME` to temporary folders.

## Golden views

The tests in `internal/ui` compare the screen with the files in `internal/ui/testdata/*.golden`.

- Run `make golden` only when the view had to change.
- Read the diff of each golden file before you commit it. A golden diff that you did not expect is a bug.
- The README shows `docs/assets/demo.gif`. Run `make demo` after a big view change, and commit the new file.

## Fixtures: fake data only

Files in `testdata/`, tests, golden views, and docs must not contain real host names, IP addresses, user names, key paths, or home folder paths. Use `example.com`, `example.net`, private address ranges, and invented names.

## Coding rules

- Keep it simple. Prefer the smaller and more obvious solution.
- Add no dependency without a clear benefit. Say what the benefit is in the pull request.
- Run no shell. Start programs with an argument list, and put `--` before a value that comes from a file.
- Pass each string from a file, from `ssh`, or from an error through `sanitize` before it reaches the screen.
- No change may weaken the safe-write flow or the consent gate. [docs/safety.md](docs/safety.md) describes both. A change to them needs tests and an update of that document.
- Use the words in [GLOSSARY.md](GLOSSARY.md) in code, comments, and docs.
- The code is Unix-only. Files that use Unix calls have a `//go:build unix` line.

## Propose a feature

Open a feature request before you write code. Describe the problem first, then the smallest change that solves it. A feature that writes to the config must use the existing writer: hash check, backup, temporary file, rename.

## Commits and pull requests

- Write short commit subjects in the imperative: `Add fold state to search`. A prefix such as `fix:` or `docs:` is welcome but not necessary.
- Keep one topic in one pull request.
- Fill in the checklist of the pull request template.

## Security problems

Do not open a public issue. See [SECURITY.md](SECURITY.md).
