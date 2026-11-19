// Package sshconfig reads OpenSSH client configuration files.
//
// The parser is read-only and source-aware. Each Block remembers the file and
// line it came from, and its raw lines. The package never writes files.
package sshconfig

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"

	"github.com/dansku/sshwave/internal/safefile"
)

// Limits for one Load. They keep a hostile or broken config from using
// unbounded time or memory.
const (
	maxIncludeDepth = 16
	maxFileBytes    = 1 << 20 // per file
	maxTotalBytes   = 8 << 20 // all files
	maxFiles        = 256
	maxRawLines     = 500 // per block
	maxBlocks       = 10000

	maxIncludeTokens = 64   // patterns read from one Include line
	maxGlobMatches   = 256  // matches read for one Include pattern
	maxIncludeWork   = 4096 // Include patterns plus their matches, for all files
)

// MaxProblems is the number of problems that one Load records. One more
// entry then says that there are more.
const MaxProblems = 100

// space is the white space that OpenSSH accepts between words on a line.
const space = " \t\r\f"

// Option is one directive inside a block.
type Option struct {
	Key   string
	Value string
	Line  int
}

// Block is one Host or Match block with the lines that belong to it.
type Block struct {
	Kind     string   // "host" or "match"
	Patterns []string // Host patterns, or the Match criteria tokens
	File     string
	Line     int // 1-based line of the Host/Match keyword
	Included bool
	Options  []Option
	Raw      []string // original lines, without trailing blank lines and without the tag stack of the next block

	// Group and Note come from the tag stack: the run of tag lines directly
	// above the Host line. Both are empty for a block that has no literal
	// alias. Group is the name of the first valid group tag, and GroupLine
	// is the 1-based line of that tag. Note holds the valid note lines, in
	// file order. TagStart is the 1-based line of the first tag in the
	// stack, or 0 without a stack.
	Group     string
	GroupLine int
	Note      []string
	TagStart  int
}

// Get returns the first value of a directive (keywords are case-insensitive).
func (b *Block) Get(key string) string {
	for _, o := range b.Options {
		if strings.EqualFold(o.Key, key) {
			return o.Value
		}
	}
	return ""
}

// Host is one literal alias. Several aliases can share one Block.
type Host struct {
	Alias string
	Block *Block
}

// Problem is a non-fatal issue found while reading.
type Problem struct {
	File string
	Line int
	Msg  string
}

// Config is the result of reading the main config and its Include files.
type Config struct {
	Main string
	// Hash is the sha256 of the main file as read. It is the zero value with
	// Hashed false when the file was not read to its end.
	Hash      [sha256.Size]byte
	Hashed    bool
	Missing   bool // the main file does not exist
	Hosts     []Host
	Rules     []*Block
	Problems  []Problem
	MatchExec bool // some Match line uses exec; ssh -G will run commands
	Untrusted bool // the parse is not complete, so MatchExec=false proves nothing
	Files     []string
	// Sum is a digest of the path and the content of each file read, in
	// order. Two loads with the same Sum read the same bytes.
	Sum [sha256.Size]byte

	seen       map[string]bool // real paths parsed in this Load
	sum        hash.Hash       // becomes Sum
	isDefault  bool            // Main is the user's ~/.ssh/config
	work       int             // Include patterns and matches in this Load
	bytes      int64           // bytes read in this Load
	blocks     int
	overBudget bool
	overBlocks bool
}

// Load reads mainPath and every file it includes. Hosts and Rules keep file
// order. Load is bounded: see the max* constants.
func Load(mainPath string) *Config {
	c := &Config{Main: mainPath, seen: map[string]bool{}, sum: sha256.New()}
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		c.Missing = true
		return c
	}
	c.isDefault = IsDefault(mainPath)
	c.parseFile(mainPath, includeDir(mainPath, c.isDefault), 0, nil, false)
	c.sum.Sum(c.Sum[:0])
	return c
}

// Changed reports whether a new load reads other files or other bytes than
// this one did. It also sees a new file that an Include now matches.
func (c *Config) Changed() bool { return Load(c.Main).Sum != c.Sum }

// IsDefault reports whether path is the user's ~/.ssh/config, also through a
// symlink. The home folder comes from os.UserHomeDir.
func IsDefault(path string) bool {
	h, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	a, errA := os.Stat(path)
	b, errB := os.Stat(filepath.Join(h, ".ssh", "config"))
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// includeDir returns the folder for relative Include paths.
//
// OpenSSH resolves them against ~/.ssh for each user config, also with -F.
// This app does the same when mainPath is the user's ~/.ssh/config (isDefault).
// For any other file it uses the folder of that file, so a separate config
// tree shows its own includes. In that case `ssh -G` can read other files than
// the app shows, so a relative Include there sets Untrusted.
func includeDir(mainPath string, isDefault bool) string {
	if h, err := os.UserHomeDir(); err == nil && isDefault {
		return filepath.Join(h, ".ssh")
	}
	return filepath.Dir(mainPath)
}

// problem records one problem. After MaxProblems it records only that there
// are more, so a hostile file cannot use much memory here.
func (c *Config) problem(file string, line int, format string, a ...any) {
	switch {
	case len(c.Problems) < MaxProblems:
		c.Problems = append(c.Problems, Problem{File: file, Line: line, Msg: fmt.Sprintf(format, a...)})
	case len(c.Problems) == MaxProblems:
		c.Problems = append(c.Problems, Problem{Msg: "more problems not shown"})
	}
}

func (c *Config) parseFile(path, sshDir string, depth int, stack []string, included bool) {
	if c.overBudget { // no more files; do no work for them
		return
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	for _, s := range stack {
		if s == real {
			c.problem(path, 0, "include loop")
			return
		}
	}
	if c.seen[real] { // already shown; this stops include fan-out
		return
	}
	if len(c.seen) >= maxFiles || c.bytes >= maxTotalBytes {
		if !c.overBudget {
			c.problem(path, 0, "limit of %d files or %d bytes reached; more files not read", maxFiles, maxTotalBytes)
		}
		c.overBudget, c.Untrusted = true, true
		return
	}
	c.seen[real] = true
	data, truncated, err := safefile.Read(path, min(maxFileBytes, maxTotalBytes-c.bytes))
	if err != nil {
		c.problem(path, 0, "cannot read: %v", err)
		c.Untrusted = true
		return
	}
	c.bytes += int64(len(data))
	hash := sha256.Sum256(data)
	c.sum.Write([]byte(path + "\x00"))
	c.sum.Write(hash[:])
	if truncated {
		c.problem(path, 0, "file too large; only the first %d bytes read", len(data))
		c.Untrusted = true
	} else if depth == 0 {
		c.Hash, c.Hashed = hash, true
	}
	c.Files = append(c.Files, path)
	stack = append(stack, real)

	var cur *Block
	flush := func() {
		if cur == nil {
			return
		}
		for len(cur.Raw) > 0 && strings.Trim(cur.Raw[len(cur.Raw)-1], space) == "" {
			cur.Raw = cur.Raw[:len(cur.Raw)-1]
		}
		c.addBlock(cur)
		cur = nil
	}
	rawFull := false
	addRaw := func(raw string, n int) bool {
		if len(cur.Raw) < maxRawLines {
			cur.Raw = append(cur.Raw, raw)
			return true
		}
		if !rawFull {
			c.problem(path, n, "block has more than %d lines; the rest is not shown", maxRawLines)
		}
		rawFull = true
		return false
	}

	// Tag lines wait in pending until the next line shows whose they are: a
	// Host line takes them as its tag stack, and any other line gives them
	// to the block above.
	var pending []string
	keep := func(n int) { // n is the line after the pending lines
		for i, raw := range pending {
			if cur != nil {
				addRaw(raw, n-len(pending)+i)
			}
		}
		pending = nil
	}

	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, raw := range lines {
		n := i + 1
		raw = strings.TrimRight(raw, "\r")
		key, val, ok := splitDirective(raw)
		if ok && strings.EqualFold(key, "host") {
			flush()
			cur = &Block{Kind: "host", Patterns: tokens(val), File: path, Line: n, Included: included, Raw: []string{raw}}
			rawFull = false
			cur.readTags(pending)
			pending = nil
			if len(cur.Patterns) == 0 {
				c.problem(path, n, "Host line has no patterns")
			}
			continue
		}
		if !ok && IsTag(raw) {
			pending = append(pending, raw)
			continue
		}
		keep(n)
		if !ok { // blank or comment
			if cur != nil {
				addRaw(raw, n)
			}
			continue
		}
		if strings.EqualFold(key, "match") {
			flush()
			cur = &Block{Kind: "match", Patterns: tokens(val), File: path, Line: n, Included: included, Raw: []string{raw}}
			rawFull = false
			// Any token with "exec" counts: exec, !exec, exec=cmd. A false
			// alarm only makes the warning stronger.
			if strings.Contains(strings.ToLower(val), "exec") {
				c.MatchExec = true
			}
			continue
		}
		if cur != nil && addRaw(raw, n) {
			cur.Options = append(cur.Options, Option{Key: key, Value: val, Line: n})
		}
		if strings.EqualFold(key, "include") {
			c.include(val, path, n, sshDir, depth, stack)
		}
	}
	keep(len(lines) + 1)
	flush()
}

// readTags sets the group and the note of b from stack, the tag lines directly
// above its Host line. A tag that is not valid is skipped, and so is each note
// line after MaxNoteLines.
func (b *Block) readTags(stack []string) {
	if len(stack) == 0 {
		return
	}
	b.TagStart = b.Line - len(stack)
	for i, line := range stack {
		if g, ok := ParseGroupTag(line); ok && b.Group == "" {
			b.Group, b.GroupLine = g, b.TagStart+i
		}
		if t, ok := ParseNoteTag(line); ok && len(b.Note) < MaxNoteLines {
			b.Note = append(b.Note, t)
		}
	}
	for len(b.Note) > 0 && b.Note[len(b.Note)-1] == "" {
		b.Note = b.Note[:len(b.Note)-1]
	}
}

func (c *Config) include(val, from string, line int, sshDir string, depth int, stack []string) {
	if depth+1 > maxIncludeDepth {
		c.problem(from, line, "include depth limit (%d) reached", maxIncludeDepth)
		c.Untrusted = true
		return
	}
	pats := fields(val, maxIncludeTokens+1)
	if len(pats) > maxIncludeTokens {
		c.problem(from, line, "include line has more than %d patterns; the rest is not read", maxIncludeTokens)
		c.Untrusted = true
		pats = pats[:maxIncludeTokens]
	}
	for _, p := range pats {
		if c.spend(1, from, line) {
			return
		}
		if strings.ContainsAny(p, "$%") {
			// OpenSSH expands ${VAR} and %-tokens here. This parser does not.
			c.problem(from, line, "include %s uses a variable or token; not read", p)
			c.Untrusted = true
			continue
		}
		if strings.HasPrefix(p, "~") && p != "~" && !strings.HasPrefix(p, "~/") {
			// OpenSSH reads the home folder of that user. This parser does not.
			c.problem(from, line, "include %s uses the home of another user; not read", p)
			c.Untrusted = true
			continue
		}
		p = expandHome(p)
		if !filepath.IsAbs(p) {
			if !c.isDefault { // ssh resolves it against ~/.ssh, and can read another file
				c.Untrusted = true
			}
			p = filepath.Join(sshDir, p)
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			c.problem(from, line, "bad include pattern %q", p)
			c.Untrusted = true
			continue
		}
		if c.spend(len(matches), from, line) {
			return
		}
		if len(matches) > maxGlobMatches {
			c.problem(from, line, "include %s has more than %d matches; the rest is not read", p, maxGlobMatches)
			c.Untrusted = true
			matches = matches[:maxGlobMatches]
		}
		if len(matches) == 0 {
			if !strings.ContainsAny(p, "*?[") {
				c.problem(from, line, "missing included file %s", p)
			}
			continue
		}
		for _, m := range matches { // Glob returns lexical order
			if c.overBudget {
				return
			}
			if st, err := os.Stat(m); err != nil || st.IsDir() {
				continue
			}
			c.parseFile(m, sshDir, depth+1, stack, true)
		}
	}
}

// spend counts n units of Include work. It reports whether the load must
// stop reading Include files, because a limit was reached now or before.
func (c *Config) spend(n int, from string, line int) bool {
	if c.overBudget {
		return true
	}
	if c.work += n; c.work > maxIncludeWork {
		c.problem(from, line, "limit of %d include patterns and matches reached; more files not read", maxIncludeWork)
		c.overBudget, c.Untrusted = true, true
	}
	return c.overBudget
}

func (c *Config) addBlock(b *Block) {
	if c.blocks >= maxBlocks || len(c.Hosts) >= maxBlocks {
		if !c.overBlocks {
			c.problem(b.File, b.Line, "more than %d blocks or hosts; the rest is not shown", maxBlocks)
		}
		c.overBlocks = true
		return
	}
	c.blocks++
	if room := maxBlocks - len(c.Hosts); len(b.Patterns) > room {
		c.problem(b.File, b.Line, "more than %d blocks or hosts; the rest is not shown", maxBlocks)
		c.overBlocks = true
		b.Patterns = b.Patterns[:room]
	}
	if b.Kind == "match" {
		c.Rules = append(c.Rules, b)
		return
	}
	isRule, isHost := false, false
	for _, p := range b.Patterns {
		if IsPattern(p) {
			isRule = true
			continue
		}
		isHost = true
		c.Hosts = append(c.Hosts, Host{Alias: p, Block: b})
	}
	if !isHost { // a Rule gets no group and no note
		b.Group, b.GroupLine, b.Note, b.TagStart = "", 0, nil, 0
	}
	if isRule {
		c.Rules = append(c.Rules, b)
	}
}

// IsPattern reports whether a Host token is a wildcard or negated pattern.
func IsPattern(p string) bool { return strings.ContainsAny(p, "*?!") }

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

// splitDirective splits a line into keyword and value. It returns ok=false for
// blank lines and comments. Like OpenSSH, it accepts a quoted keyword and
// "keyword=value".
func splitDirective(line string) (key, val string, ok bool) {
	s := strings.Trim(line, space)
	if s == "" || s[0] == '#' {
		return "", "", false
	}
	if s[0] == '"' {
		key, s, _ = strings.Cut(s[1:], "\"")
	} else if i := strings.IndexAny(s, space+"="); i >= 0 {
		key, s = s[:i], s[i:]
	} else {
		return s, "", true
	}
	val = strings.Trim(s, space)
	val = strings.Trim(strings.TrimPrefix(val, "="), space)
	return key, stripComment(val), true
}

// stripComment removes a trailing " # comment" that is outside quotes.
func stripComment(v string) string {
	inQ := false
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '"':
			inQ = !inQ
		case '#':
			if !inQ && (i == 0 || strings.IndexByte(space, v[i-1]) >= 0) {
				return strings.Trim(v[:i], space)
			}
		}
	}
	return v
}

// tokens splits a value on white space and keeps double-quoted parts together.
func tokens(v string) []string { return fields(v, 0) }

// fields is tokens with a limit: with max > 0 it returns the first max tokens
// only, and does not read the rest of v.
func fields(v string, max int) []string {
	var out []string
	var b strings.Builder
	inQ, has := false, false
	for _, r := range v {
		switch {
		case r == '"':
			inQ, has = !inQ, true
		case strings.ContainsRune(space, r) && !inQ:
			if has {
				out = append(out, b.String())
				if len(out) == max {
					return out
				}
				b.Reset()
				has = false
			}
		default:
			b.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, b.String())
	}
	return out
}
