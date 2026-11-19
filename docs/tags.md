# Tag format

sshwave stores the Group and the Note of a host as comments in your OpenSSH config. OpenSSH ignores them. This page gives the exact format, so other tools can read and write it. The code is in `internal/sshconfig/group.go`, `note.go`, and `parser.go`.

## Example

```
# sshwave: group=Work
# sshwave: note=Primary API box.
# sshwave: note=Deploys run at 02:00 UTC.
Host production-api prod
    HostName 192.168.1.50
```

## Tag lines

A tag line is a comment line with one of two exact prefixes:

| Tag | Line |
| --- | --- |
| Group | `# sshwave: group=<name>` |
| Note | `# sshwave: note=<text>` |

- The prefix is case-sensitive and exact: `#`, one space, `sshwave:`, one space, then `group=` or `note=`.
- White space before the `#` is permitted: space, tab, carriage return, form feed.
- Lines end with `\n` or `\r\n`.

## Tag stack

The tag stack of a `Host` line is the run of tag lines directly above it.

- Any other line ends the stack: a blank line, a plain comment, or a directive.
- A tag line that is not in the stack of a `Host` line is a plain comment.
- Group tags and note tags can be in any order. sshwave writes a new group tag at the top of the stack and new note tags at the bottom.
- Tags count only for a `Host` line with at least one literal name. A name is literal when it has none of `*`, `?`, `!`. All literal names of the line share the tags. Tags above a `Match` line or above a `Host` line with only patterns have no effect.

## Group

- The name is the text after `group=`, without the white space around it.
- A valid name is not empty, is valid UTF-8, has 64 bytes at most, and has no control character (C0, DEL, C1).
- The first valid group tag in the stack counts. A reader skips a tag that is not valid and each later group tag.
- To set a group, sshwave replaces all group tags of the stack, valid or not, with one tag. To remove the group, it removes them all.

## Note

- Each note tag is one line of the note, in file order. The text is what comes after `note=`, without the white space around it.
- A valid line is valid UTF-8, has 200 bytes at most, and has no control character. A tab inside the text makes the line not valid. An empty line is valid.
- A reader skips a tag that is not valid. It reads 10 valid lines at most and skips the rest. It then drops empty lines at the end.
- A note has 10 lines at most, counted after the empty lines at the end are dropped. sshwave refuses to write a longer note.
- To set a note, sshwave replaces all note tags of the stack, valid or not. To remove the note, it removes them all.

## Rules for a writer

- Change only tag lines in the stack. Leave all other bytes as they are.
- Give a new tag line the indentation and the line ending of the `Host` line, or of the tag line that it replaces.
- Refuse a value with a control character. This is what stops a value from adding a line to the config.
- A note is plain text in a config file. Do not put a secret in it.
