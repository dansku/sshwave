package sshconfig

import (
	"strings"
	"testing"
)

const noteFixture = "../../testdata/ssh-notes/config"

func TestNoteFixture(t *testing.T) {
	c := Load(noteFixture)
	for alias, want := range map[string]string{
		"github":         "Deploy key only",
		"production-api": "Primary API box. Ask team before a restart.||Deploys run at 02:00 UTC.",
		"prod":           "Primary API box. Ask team before a restart.||Deploys run at 02:00 UTC.",
		"staging-api":    "Rebuilt each night",
		"raspberry-pi":   "Under the desk. The fan is loud.",
		"scratch":        "", // a plain comment is not a note
	} {
		if got := strings.Join(hostBlock(t, c, alias).Note, "|"); got != want {
			t.Errorf("%s: note %q, want %q", alias, got, want)
		}
	}
	if b := hostBlock(t, c, "github"); !b.Included || b.TagStart != 1 || b.Line != 2 {
		t.Errorf("included: included=%v tagStart=%d line=%d", b.Included, b.TagStart, b.Line)
	}
	if a, b := hostBlock(t, c, "production-api"), hostBlock(t, c, "prod"); a != b || a.TagStart != 4 || a.GroupLine != 4 || a.Line != 8 || a.Group != "Work" {
		t.Errorf("multi-alias block: same=%v %+v", a == b, a)
	}
	// Group and note in the other order.
	if b := hostBlock(t, c, "staging-api"); b.Group != "Work" || b.TagStart != 12 || b.GroupLine != 13 || b.Line != 14 {
		t.Errorf("staging-api: %+v", b)
	}
	if b := hostBlock(t, c, "scratch"); b.TagStart != 0 || strings.Join(b.Raw, "|") != "Host scratch|    HostName 10.0.0.99" {
		t.Errorf("scratch: %+v", b)
	}
	for _, r := range c.Rules {
		if r.Note != nil || r.TagStart != 0 {
			t.Errorf("rule %v has note %q", r.Patterns, r.Note)
		}
	}
	if got := strings.Join(c.Groups(), ","); got != "Work" {
		t.Errorf("groups = %s", got)
	}
	if len(c.Problems) != 0 {
		t.Errorf("problems %v", c.Problems)
	}
}

func TestTagStackForms(t *testing.T) {
	long := strings.Repeat("x", MaxNoteLine)
	many := strings.Repeat("# sshwave: note=n\n", MaxNoteLines+2)
	for text, want := range map[string]string{ // "group/note lines joined by |"
		"# sshwave: note=one\nHost a\n":                                                    "/one",
		"# sshwave: note=one\n# sshwave: note=two\nHost a\n":                               "/one|two",
		"# sshwave: group=G\n# sshwave: note=one\nHost a\n":                                "G/one",
		"# sshwave: note=one\n# sshwave: group=G\n# sshwave: note=two\nHost a\n":           "G/one|two",
		"# sshwave: group=G\n# sshwave: group=H\n# sshwave: note=n\nHost a\n":              "G/n", // the first group counts
		"# sshwave: group=\n# sshwave: group=H\nHost a\n":                                  "H/",  // the first valid one
		"# sshwave: note=one\r\n# sshwave: note=two\r\nHost a\r\n":                         "/one|two",
		"\t # sshwave: note=  one \t\n\t Host a\n":                                         "/one",
		"# sshwave: note=one\n# sshwave: note=\n# sshwave: note=three\nHost a\n":           "/one||three",
		"# sshwave: note=one\n# sshwave: note=\n# sshwave: note=  \nHost a\n":              "/one", // blank lines at the end
		"# sshwave: note=\n# sshwave: note=\nHost a\n":                                     "/",    // no text, no note
		"# sshwave: note=" + long + "\nHost a\n":                                           "/" + long,
		"# sshwave: note=# sshwave: group=G\nHost a\n":                                     "/# sshwave: group=G",
		"# sshwave: note=ok\n# sshwave: note=" + long + "x\n# sshwave: note=end\nHost a\n": "/ok|end", // too long
		"# sshwave: note=a\x1b[2Jb\n# sshwave: note=ok\nHost a\n":                          "/ok",     // control character
		"# sshwave: note=a\tb\n# sshwave: note=a\rb\n# sshwave: note=\xff\nHost a\n":       "/",
		many + "Host a\n": "/" + strings.TrimSuffix(strings.Repeat("n|", MaxNoteLines), "|"),

		"# sshwave: note=far\n\nHost a\n":                       "/", // a blank line ends the stack
		"# sshwave: note=far\n\n# sshwave: group=G\nHost a\n":   "G/",
		"# sshwave: note=far\n# c\n# sshwave: note=n\nHost a\n": "/n", // so does a comment
		"# sshwave: Note=n\nHost a\n":                           "/",  // the key is case-sensitive
		"#sshwave: note=n\nHost a\n":                            "/",
		"# note\nHost a\n":                                      "/",
		"Host a\n# sshwave: note=below\n":                       "/",
		"# sshwave: note=n\nMatch all\nHost a\n":                "/",
	} {
		c := loadText(t, text)
		b := hostBlock(t, c, "a")
		if got := b.Group + "/" + strings.Join(b.Note, "|"); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
		if len(c.Problems) != 0 {
			t.Errorf("%q: problems %v", text, c.Problems)
		}
	}
}

func TestNoteOnlyForHosts(t *testing.T) {
	c := loadText(t, "# sshwave: note=rule\nHost *.x\n# sshwave: note=both\nHost b *.y\n# sshwave: note=m\nMatch all\n")
	if len(c.Rules) != 3 || c.Rules[0].Note != nil || strings.Join(c.Rules[1].Note, "|") != "both" || c.Rules[2].Note != nil {
		t.Errorf("rules: %+v", c.Rules)
	}
}

func TestCleanNote(t *testing.T) {
	long := strings.Repeat("x", MaxNoteLine)
	ten := strings.Split(strings.Repeat("n,", MaxNoteLines-1)+"n", ",")
	for _, tc := range []struct {
		in   []string
		want string // lines joined by |
	}{
		{nil, ""},
		{[]string{""}, ""},
		{[]string{"", " ", ""}, ""},
		{[]string{"  a  ", "", "b", "", "  "}, "a||b"},
		{[]string{"", "a"}, "|a"},
		{[]string{"  " + long + "  "}, long}, // the limit is for the stored text
		{[]string{"héllo ✎ 日本"}, "héllo ✎ 日本"},
		{append(ten, "", " "), strings.Join(ten, "|")}, // blank lines at the end do not count
	} {
		got, err := CleanNote(tc.in)
		if err != nil || strings.Join(got, "|") != tc.want {
			t.Errorf("%q: got %q, %v", tc.in, got, err)
		}
	}
	for _, bad := range [][]string{
		{"a\nb"}, {"a\r"}, {"\na"}, {"a\tb"}, {"\ta"}, {"a\x00"}, {"a\x1b[2J"}, {"a\u0085b"}, {"\x7f"},
		{"\xff"}, {long + "x"}, {"ok", long + "x"}, append(ten, "n"),
	} {
		if got, err := CleanNote(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
	if _, err := CleanNote([]string{"ok", "a\tb"}); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error must name the line: %v", err)
	}
}
