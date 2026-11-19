# VHS script for the README demo. See https://github.com/charmbracelet/vhs
# Run it from the repository root with `make demo`. It writes docs/assets/demo.gif.
#
# Safety rules for this script:
# - It uses a temporary HOME with a copy of the fixture as ~/.ssh/config. It
#   never reads or writes your real config or backup folder, and the screen
#   shows ~/.ssh/config, not a real home folder path.
# - It never presses `y` in the consent dialog, never presses `c`, and never
#   presses Enter on a host. OpenSSH ignores $HOME, so those keys would use
#   your real SSH setup.

Output docs/assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 12
Set TypingSpeed 60ms

Hide
Type `export HOME="$(cd "$(mktemp -d)" && pwd -P)" PATH="$PWD:$PATH" PS1='$ '`
Enter
Type `export XDG_DATA_HOME="$HOME/.local/share" && mkdir -p "$HOME/.ssh" && cp -R testdata/ssh-notes/. "$HOME/.ssh/" && clear`
Enter
Show

Type "sshwave"
Sleep 500ms
Enter
Sleep 2s

# Browse: hosts in the group Work.
Type "j"
Sleep 700ms
Type "j"
Sleep 700ms
Type "j"
Sleep 1.5s

# Search, then clear the search.
Type "/"
Sleep 500ms
Type "pi"
Sleep 2s
Escape
Sleep 1s

# Fold and unfold the group Work. The cursor is on its header.
Space
Sleep 1.2s
Space
Sleep 1.2s

# The note of production-api. Esc closes the dialog without a save.
Type "j"
Sleep 700ms
Type "n"
Sleep 2.5s
Escape
Sleep 800ms

# The group dialog. Esc closes it without a change.
Type "m"
Sleep 2s
Escape
Sleep 800ms

# The consent dialog for ssh -G. `n` cancels: no command runs.
Type "x"
Sleep 3s
Type "n"
Sleep 800ms

# Help, then quit.
Type "?"
Sleep 2.5s
Escape
Sleep 500ms
Type "q"
Sleep 500ms
