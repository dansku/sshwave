package sshconfig

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A group is a label on one Host block. It is stored as one tag comment in
// the tag stack: the run of tag lines directly above the Host line.
//
//	# sshwave: group=Name
//
// OpenSSH reads the tag as a comment. When a stack has several group tags,
// the first valid one counts.

// groupTagPrefix starts a tag line, after optional indentation.
const groupTagPrefix = "# sshwave: group="

// MaxGroupName is the longest group name, in bytes.
const MaxGroupName = 64

// ValidGroupName reports whether name can be stored in a tag line. A control
// character is refused, so a name can never add a line to the config.
func ValidGroupName(name string) error {
	switch {
	case name == "":
		return errors.New("group name is empty")
	case name != strings.TrimSpace(name):
		return errors.New("group name starts or ends with white space")
	case len(name) > MaxGroupName:
		return fmt.Errorf("group name is longer than %d bytes", MaxGroupName)
	case !utf8.ValidString(name):
		return errors.New("group name is not valid UTF-8")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("group name has a control character")
		}
	}
	return nil
}

// GroupTag returns the tag line for name, without indentation or line ending.
func GroupTag(name string) string { return groupTagPrefix + name }

// IsGroupTag reports whether line is a tag line, also one with a name that
// ParseGroupTag refuses. The line can have indentation and a trailing CR.
func IsGroupTag(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, space), groupTagPrefix)
}

// ParseGroupTag returns the group name of a tag line. ok is false when line
// is not a tag line or the name is not valid.
func ParseGroupTag(line string) (name string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimLeft(line, space), groupTagPrefix)
	if !found {
		return "", false
	}
	name = strings.TrimSpace(rest)
	if ValidGroupName(name) != nil {
		return "", false
	}
	return name, true
}

// HostPatterns returns the patterns of a Host line. ok is false when line is
// not a Host line.
func HostPatterns(line string) (patterns []string, ok bool) {
	key, val, ok := splitDirective(strings.TrimRight(line, "\r"))
	if !ok || !strings.EqualFold(key, "host") {
		return nil, false
	}
	return tokens(val), true
}

// Groups returns the group names of the hosts, in first-seen file order.
func (c *Config) Groups() []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		if g := h.Block.Group; g != "" && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}
