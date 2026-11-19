# sshwave

A terminal app to browse the hosts in an OpenSSH config file and to edit that file safely, with an automatic backup before each edit.

## Language

**Host alias**:
A literal name from a `Host` line that you can pass to `ssh`. One `Host` line with several names gives several Host aliases.
_Avoid_: Host entry, connection, server

**Rule**:
A `Host` pattern with a wildcard or negation, or a `Match` block. A Rule applies settings to other hosts and is not itself a Host alias.
_Avoid_: Wildcard host, pattern host

**Group**:
A label on one `Host` block, stored as a tag comment above its `Host` line. A block has one Group at most, and all its Host aliases share it. The app writes no Group for a Rule or for a host from an Included file.
_Avoid_: Folder, section, category, tag

**Note**:
Free text attached to one `Host` block, stored as tag comments above its `Host` line. Only tagged comments count. All Host aliases of the block share it. The app writes no Note for a Rule or for a host from an Included file.
_Avoid_: Comment, description, memo

**Main config**:
The config file that the app opens: `~/.ssh/config`, unless the user gives another file. It is the only file this app edits and backs up.
_Avoid_: Source of truth, ssh config (ambiguous with included files)

**Included file**:
A file that the Main config pulls in with `Include`. The app shows its hosts but does not edit it.
_Avoid_: Sub-config, imported file

**Backup**:
An exact copy of the Main config, saved before each edit, with a timestamp.
_Avoid_: Snapshot, version, revision

**Effective configuration**:
The final settings that OpenSSH computes for one Host alias, as reported by `ssh -G`.
_Avoid_: Resolved config, merged config

**Consent**:
The user's permission to run `ssh -G` for the current load of the config. It ends at each edit, at each reload, and when a file of that load has changed on disk.
_Avoid_: Trust, approval, allow-list
