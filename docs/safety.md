# Safety model

sshwave works on a file that controls how you reach your servers. This page says exactly what the app reads, runs, and writes, and what can still go wrong. The words Host alias, Rule, Group, Note, Main config, Included file, Backup, and Consent are defined in [GLOSSARY.md](../GLOSSARY.md).

## What the app reads

- The Main config: `~/.ssh/config`, or the file from `-config`.
- Each file that it pulls in with `Include`. Globs and `~/` are expanded. An `Include` with `$` or `%`, or with `~user`, is not read, and the app counts a problem.
- Regular files only. A FIFO or a device is refused and cannot block the app.

Limits for one load:

| Limit | Value |
| --- | --- |
| Size of one file | 1 MiB (the app reads the first 1 MiB of a larger file) |
| Size of all files | 8 MiB |
| Number of files | 256 |
| `Include` depth | 16 |
| Patterns on one `Include` line | 64 (the app does not read the rest of the line) |
| Glob matches for one `Include` pattern | 256 (the first in name order) |
| `Include` patterns plus their glob matches, for all files | 4096 |
| Problems recorded | 100, then one entry that says there are more |
| Lines shown for one block | 500 |
| Blocks, and Host aliases | 10000 each |

When a limit is reached, the header shows a problem count. Above 100 it shows `100+ problems`. When the file limit, the byte limit, or the 4096 limit is reached, the app reads no more `Include` files in that load. Reading runs no program and changes no file.

Relative `Include` paths resolve against `~/.ssh` when the Main config is your `~/.ssh/config`. With `-config` on another file, they resolve against the folder of that file. OpenSSH always uses `~/.ssh`, so in that case `ssh` can read other files than the app shows.

The warning before `ssh -G` is stronger ("This config was not fully read.") in each of these cases:

- A file was not read, or not read to its end: a read error, the size limit, the file limit, the byte limit, or the depth limit.
- An `Include` limit from the table was reached.
- An `Include` has `$` or `%`, has `~user`, or has a pattern that is not valid.
- An `Include` has a relative path, and the Main config is not your `~/.ssh/config`.

## When `ssh -G` runs

The Effective configuration comes from `ssh -G -- <alias>` when the Main config is your `~/.ssh/config`, and from `ssh -F <config> -G -- <alias>` for each other file. `ssh -G` connects to no host, but it can run local commands from `Match exec`.

The `-F` rule is the same for `ssh -G` and for `c`:

- Default path: the Main config is `<home>/.ssh/config`, also through a symlink. `<home>` is `$HOME`, as Go's `os.UserHomeDir` gives it. The app passes no `-F`. `ssh` then reads your config and `/etc/ssh/ssh_config`, as it does when you run it yourself.
- Other path: the app passes `-F <config>`. `ssh` then reads only that file and its `Include` files. It ignores `/etc/ssh/ssh_config`.

- The app never runs it without Consent. You give Consent with `x`, then `y`. Only `y` accepts. `Enter` does not.
- Consent is for the current load only. It ends at each edit and each reload.
- Consent is for the bytes that the app read. Before each `ssh -G` run the app loads the config again, in memory, and compares a SHA-256 digest of the path and content of each file read. If a file changed, or the set of files changed, `ssh -G` does not run, Consent ends, and the app loads the config again and tells you.
- The warning is stronger when the app finds `Match exec`, and in the cases of the list above.
- It runs for the selected Host alias only, after a short pause, and the app keeps the result until the next load.
- It runs without a shell. An alias that is empty, starts with `-`, or has ASCII white space or control characters is refused.
- It stops after 5 seconds. The app then kills `ssh` and the commands it started (one process group).
- The app keeps 1 MiB of standard output and 1 MiB of standard error at most. When `ssh -G` writes more, the app kills it in the same way and shows an error, not a result.

## When `ssh` connects

`c` runs `ssh -- <alias>` (default path) or `ssh -F <config> -- <alias>` (other path) for the selected Host alias in your terminal. The app comes back when `ssh` exits.

- There is no Consent step. The key press is the request.
- `ssh` then applies your whole config, with `Match exec`, `ProxyCommand`, `LocalCommand`, and all other settings.
- `c` does nothing while an edit is in progress or when the Main config does not exist.

## Backups

Each edit starts with a Backup: an exact copy of the Main config.

- Folder: `$XDG_DATA_HOME/sshwave/backups/`. The default is `~/.local/share/sshwave/backups/`, also on macOS. A relative `XDG_DATA_HOME` is ignored.
- Name: `<UTC time>-<file name>-<hash>.bak`, for example `20261119T101500.123456789Z-config-1a2b3c4d.bak`. The hash is the first 8 hex digits of the SHA-256 of the real path of the file. A file name longer than 150 bytes is cut.
- Mode `0600`. The `sshwave/` and `backups/` folders must be real directories that you own. The app sets them to `0700`.
- The app reads the copy back and compares it. If the Backup fails, the edit does not start.
- The app keeps the newest 50 Backups for each config path and deletes older ones. It deletes them only after an edit that changed the file, when the new Backup stays. An edit that fails or changes nothing deletes no older Backup.
- Files larger than 16 MiB and files that are not regular are refused, for the editor and for a tag write.
- If the editor changes nothing, the app deletes that Backup. If it changed the file, the app keeps the Backup and then deletes Backups above 50.

## Edit in your editor

`Enter` or `e` makes a Backup and then opens the Main config in `$VISUAL`, then `$EDITOR`, then `vi`. The app splits the editor value on white space and starts it without a shell, with the absolute real path of the file. After the editor exits, the app loads the config again.

## Tag writes: Group and Note

`m` and `n` are the only actions where the app itself writes to the Main config. The steps:

1. Resolve the real path (a symlink is followed) and read the file.
2. Compare its SHA-256 with the one from the last load. If they differ, write nothing and load again.
3. Check that the expected `Host` line is at the expected line number.
4. Build the new content. Only tag lines directly above that `Host` line change. All other bytes stay the same, also the line endings.
5. If nothing changes, stop. No Backup, no write.
6. Make the Backup. If it fails, write nothing.
7. Write a temporary file in the same folder, give it the permission bits of the config, and sync it.
8. Rename it over the config, and sync the folder. If the write or the rename fails, the config is unchanged, and the app removes the temporary file and the new Backup.
9. Delete Backups of this config above the newest 50. This step runs only after step 8 worked.

The app writes no tag for a Rule, for a host from an Included file, or when the Main config was not read to its end.

## Tag format and limits

```
# sshwave: group=Work
# sshwave: note=Primary API box.
# sshwave: note=Deploys run at 02:00 UTC.
Host production-api prod
```

- Group name: 1 to 64 bytes, valid UTF-8, no control characters, no white space at its start or end. One Group for each `Host` block.
- Note: 10 lines at most, 200 bytes for each line at most, valid UTF-8, no control characters. The app removes spaces around a line and empty lines at the end.
- A value with a control character is refused, so a value can never add a line to your config.
- A Note is plain text in your config. Do not store a password in it.

[tags.md](tags.md) has the full grammar.

## What can still go wrong

- **A change between check and rename.** If another program writes the config in the short time between step 2 and step 8, that change is lost. The Backup holds the content from step 1.
- **File identity.** The rename makes a new file. A hard link to the old file, its extended attributes, and a different owner are not kept. The permission bits are kept. A read-only config is replaced too, when the folder permits it.
- **Editor quoting.** `$VISUAL` and `$EDITOR` are split on white space. A path or an argument with a space does not work.
- **`ssh -G` after a quit.** If you quit while `ssh -G` runs, that process can continue until it ends by itself.
- **A change between check and `ssh -G`.** The app compares the files and then starts `ssh -G`. A file that changes in that short time is not seen.
- **A file larger than 1 MiB.** The app reads and compares only the first 1 MiB. A change after that point does not end Consent. Such a load always has the stronger warning.
- **Files that the app does not read.** With the default path, `ssh` also reads `/etc/ssh/ssh_config` and its `Include` files. The app does not show them and does not compare them.
- **`$HOME` is not your real home.** The app finds `~/.ssh/config` from `$HOME`. OpenSSH uses the home folder from the user database. If they differ, the app passes no `-F` for `$HOME/.ssh/config`, and `ssh` reads the config of the real home.
- **Many files in one folder.** The app limits glob matches, not the size of a folder. A glob in a very large folder can still be slow.
- **Network use.** After Consent, `ssh -G` can do DNS lookups for some configs (for example with `CanonicalizeHostname`).
- **The parser is not OpenSSH.** The list shows what the app reads. `ssh -G` and `ssh` are the authority. They can read files that the app did not, for example an `Include` with a variable.
- **Problems are counted, not listed.** The header shows how many problems the read had, not which. The app records 100 at most.
- **Backups are plain copies.** They are as sensitive as the config, and they are not encrypted. Only 50 are kept.
- **Your user account.** The app does not defend against other processes of your user, or against a hostile `ssh` or editor in your `PATH`.

## Restore a Backup by hand

The app has no restore screen yet. The names sort by time, so the last one is the newest:

```sh
ls ~/.local/share/sshwave/backups/

# Optional: keep the current file.
cp -p ~/.ssh/config ~/.ssh/config.before-restore

cp ~/.local/share/sshwave/backups/20261119T101500.123456789Z-config-1a2b3c4d.bak ~/.ssh/config
chmod 600 ~/.ssh/config
```

If `XDG_DATA_HOME` is set to an absolute path, look in `$XDG_DATA_HOME/sshwave/backups/`.
