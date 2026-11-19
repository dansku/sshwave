# Security Policy

## Report a vulnerability

Report it in private. Do not open a public issue.

Use GitHub private vulnerability reporting: open the [Security tab](https://github.com/dansku/sshwave/security) of the repository and select **Report a vulnerability**, or go directly to the [advisory form](https://github.com/dansku/sshwave/security/advisories/new).

Include the version (`sshwave -version`), your OS, and the smallest config that shows the problem. Use fake host data.

## Supported versions

Only the latest release gets security fixes.

## Scope

In scope:

- A config file, an `Include` file, or `ssh` output that makes the app run a command, write a file, or print terminal control sequences without your action.
- A write that damages the config or loses data, or a backup that other users can read.
- A way to run `ssh -G` without Consent.

Out of scope:

- Problems in OpenSSH, in your editor, or in your terminal.
- What `ssh` does after you allow `ssh -G` or after you connect with `c`.
- An attacker who already controls your user account or your `PATH`.

## Threat model

1. The config and its `Include` files are untrusted input. A file can come from a shared repository or a tool.
2. Reading is passive: regular files only, with size, file, depth, block, and `Include` work limits. Reading runs no program.
3. `ssh -G` can run local commands (`Match exec`). The app runs it only after Consent, for the selected Host alias, with a 5 second limit and a 1 MiB output limit.
4. Consent ends at each edit and each reload, because the file can be different then. It also ends when the app finds, before an `ssh -G` run, that a file it read has changed.
5. `c` starts a real `ssh` connection to the selected Host alias. The key press is the request; there is no second question.
6. The app starts `ssh` and the editor without a shell, and puts `--` before the Host alias.
7. Text from files, from `ssh -G`, and from errors is cleaned before it reaches the terminal: control and bidirectional characters become `·`.
8. A tag write never adds a line: values with control characters are refused. It checks the file hash, makes a Backup, then replaces the file atomically.
9. Backups are private: files `0600`, folders `0700`. The two backup folders must be real directories that you own, not symlinks.
10. Not defended: other processes of your user, a hostile `ssh` or editor binary, and secrets that you put in a Note.

[docs/safety.md](docs/safety.md) has the details and the residual risks.
