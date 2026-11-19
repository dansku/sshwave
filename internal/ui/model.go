// Package ui is the Bubble Tea interface. It holds no SSH logic.
package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dansku/sshwave/internal/edit"
	"github.com/dansku/sshwave/internal/sshconfig"
	"github.com/dansku/sshwave/internal/sshedit"
)

type dialog int

const (
	dlgNone dialog = iota
	dlgHelp
	dlgConsent
	dlgMove
	dlgNote
)

const (
	stInfo = iota
	stOK
	stWarn
	stErr
)

// row is one selectable line of the list: a host, a rule, or a group header.
type row struct {
	host *sshconfig.Host
	rule *sshconfig.Block

	head  bool   // a group header
	group string // the group of a header; "" is the header of Ungrouped
	count int    // hosts below a header
}

// block returns the block of a host or rule row, and nil for a header.
func (r row) block() *sshconfig.Block {
	if r.host != nil {
		return r.host.Block
	}
	return r.rule
}

// rowID names a host row or a header row, so the cursor can return to it
// after the rows change.
type rowID struct {
	head bool
	name string // alias, or group of a header
}

func (r row) id() (rowID, bool) {
	switch {
	case r.head:
		return rowID{true, r.group}, true
	case r.host != nil:
		return rowID{false, r.host.Alias}, true
	}
	return rowID{}, false
}

const ungrouped = "Ungrouped"

// groupLabel returns the name to show for a group. "" is no group.
func groupLabel(g string) string {
	if g == "" {
		return ungrouped
	}
	return sanitize(g)
}

const (
	mvGroup = iota // an existing group
	mvNone         // Ungrouped
	mvNew          // type a name
)

type moveItem struct {
	kind int
	name string
}

// moveState is the state of the move dialog.
type moveState struct {
	alias    string
	line     int      // the Host line of the block
	patterns []string // the patterns of that line
	from     string   // the current group
	items    []moveItem
	cur      int
	typing   bool // the name input is open
	name     string
	err      string
}

// noteState is the state of the note dialog: a small text editor.
type noteState struct {
	alias    string
	line     int      // the Host line of the block
	patterns []string // the patterns of that line
	from     []string // the current note
	lines    []string // the text; it always has one line or more
	row, col int      // the cursor; col counts runes
}

type effState struct {
	pairs   []sshconfig.Pair
	err     error
	loading bool
}

// connectDoneMsg reports that an ssh session ended.
type connectDoneMsg struct {
	alias string
	err   error
}

// Model is the root Bubble Tea model.
type Model struct {
	path   string
	cfg    *sshconfig.Config
	loaded bool
	w, h   int

	query     string
	filtering bool
	rows      []row
	nTop      int             // rows above the rules: hosts and group headers
	nHosts    int             // hosts that match the query
	folded    map[string]bool // by group; kept in memory only
	cursor    int
	offset    int

	eff     map[string]*effState
	consent bool // the user allowed `ssh -G` for the current load
	editing bool // from an edit start to the next load; no consent, no `ssh -G`
	dlg     dialog
	mv      moveState
	nt      noteState
	seq     int

	status     string
	statusKind int
}

// New returns a model for the config file at path.
func New(path string) Model {
	return Model{path: path, eff: map[string]*effState{}, folded: map[string]bool{}, w: 100, h: 30}
}

type (
	loadedMsg  struct{ cfg *sshconfig.Config }
	effTickMsg struct {
		seq   int
		alias string
	}
	effMsg struct {
		st    *effState // the loading entry that this result replaces
		alias string
		pairs []sshconfig.Pair
		err   error
	}
	// effStaleMsg says that `ssh -G` did not run, because a config file is
	// not the one that the app read.
	effStaleMsg struct {
		st    *effState // the loading entry of the run that did not start
		alias string
	}
	prepMsg struct {
		sess *edit.Session
		line int
		err  error
	}
	execDoneMsg struct {
		sess *edit.Session
		err  error
	}
	finishMsg struct {
		changed bool
		backup  string
		err     error
		runErr  error
	}
	movedMsg struct {
		alias, group string
		backup       string
		err          error
	}
	notedMsg struct {
		cleared bool
		backup  string
		err     error
	}
)

func (m Model) load() tea.Cmd {
	path := m.path
	return func() tea.Msg { return loadedMsg{sshconfig.Load(path)} }
}

// Init loads the config.
func (m Model) Init() tea.Cmd { return m.load() }

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil

	case loadedMsg:
		var prev rowID
		r, ok := m.current()
		if ok {
			prev, ok = r.id()
		}
		m.cfg, m.loaded = msg.cfg, true
		m.resetEff() // consent is for one load only
		m.editing = false
		if m.dlg == dlgConsent || m.dlg == dlgMove || m.dlg == dlgNote { // all refer to the old load
			m.dlg = dlgNone
		}
		m.rebuild()
		if ok {
			m.reselect(prev)
		}
		m.ensureVisible()
		cmd := m.scheduleEff()
		return m, cmd

	case effTickMsg:
		if msg.seq != m.seq || m.selectedAlias() != msg.alias || !m.effAllowed() {
			return m, nil
		}
		st := &effState{loading: true}
		m.eff[msg.alias] = st
		path, cfg := m.path, m.cfg
		return m, func() tea.Msg {
			// The consent is for the bytes that the app read and showed.
			if cfg.Changed() {
				return effStaleMsg{st, msg.alias}
			}
			p, err := sshconfig.Effective(path, msg.alias)
			return effMsg{st, msg.alias, p, err}
		}

	case effStaleMsg:
		if m.eff[msg.alias] != msg.st { // started before an edit or a reload
			return m, nil
		}
		m.resetEff() // the consent was for other bytes
		m.setStatus(stWarn, "config changed on disk · ssh -G did not run · reloaded · x allows it again")
		return m, m.load()

	case effMsg:
		if m.eff[msg.alias] != msg.st { // started before an edit or a reload
			return m, nil
		}
		m.eff[msg.alias] = &effState{pairs: msg.pairs, err: msg.err}
		return m, nil

	case prepMsg:
		if msg.err != nil {
			m.editing = false
			m.setStatus(stErr, msg.err.Error())
			return m, nil
		}
		c, err := edit.Command(msg.sess.Path, msg.line)
		if err != nil {
			m.editing = false
			msg.sess.Finish()
			m.setStatus(stErr, err.Error())
			return m, nil
		}
		sess := msg.sess
		return m, tea.ExecProcess(c, func(err error) tea.Msg { return execDoneMsg{sess, err} })

	case connectDoneMsg:
		if msg.err != nil {
			m.setStatus(stWarn, "ssh "+msg.alias+": "+msg.err.Error())
		} else {
			m.setStatus(stInfo, "disconnected from "+msg.alias)
		}
		return m, nil

	case execDoneMsg:
		return m, func() tea.Msg {
			changed, err := msg.sess.Finish()
			return finishMsg{changed: changed, backup: msg.sess.Backup, err: err, runErr: msg.err}
		}

	case finishMsg:
		switch {
		case msg.err != nil:
			m.setStatus(stErr, "could not re-read config: "+msg.err.Error())
		case msg.runErr != nil && msg.changed:
			m.setStatus(stWarn, "editor exited with an error · backup kept: "+short(msg.backup))
		case msg.runErr != nil:
			m.setStatus(stWarn, "editor exited with an error · no changes")
		case msg.changed:
			m.setStatus(stOK, "saved · backup: "+short(msg.backup))
		default:
			m.setStatus(stInfo, "no changes")
		}
		return m, m.load()

	case movedMsg:
		if m.wrote(fmt.Sprintf("moved %s to %s", msg.alias, groupLabel(msg.group)), msg.backup, msg.err) {
			delete(m.folded, msg.group) // show the host in its group
		}
		return m, m.load() // always: only a load ends the edit state

	case notedMsg:
		done := "note saved"
		if msg.cleared {
			done = "note cleared"
		}
		m.wrote(done, msg.backup, msg.err)
		return m, m.load()

	case tea.PasteMsg:
		if m.dlg == dlgNote {
			m.nt.insert(msg.Content)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	m.status = ""

	switch m.dlg {
	case dlgHelp:
		if k == "esc" || k == "?" || k == "q" || k == "enter" {
			m.dlg = dlgNone
		}
		return m, nil
	case dlgConsent:
		switch k {
		case "y": // not enter: it is easy to press by accident
			m.consent, m.dlg = true, dlgNone
			cmd := m.scheduleEff()
			return m, cmd
		case "n", "esc", "q":
			m.dlg = dlgNone
		}
		return m, nil
	case dlgMove:
		return m.moveKey(msg)
	case dlgNote:
		return m.noteKey(msg)
	}

	if m.filtering {
		switch k {
		case "esc":
			m.filtering, m.query = false, ""
			m.rebuild()
		case "enter":
			m.filtering = false
		case "up", "ctrl+p":
			cmd := m.move(-1)
			return m, cmd
		case "down", "ctrl+n":
			cmd := m.move(1)
			return m, cmd
		case "backspace":
			if rs := []rune(m.query); len(rs) > 0 {
				m.query = string(rs[:len(rs)-1])
				m.rebuild()
			}
		default:
			if msg.Text != "" {
				m.query += msg.Text
				m.rebuild()
			}
		}
		m.ensureVisible()
		cmd := m.scheduleEff()
		return m, cmd
	}

	switch k {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.query != "" {
			m.query = ""
			m.rebuild()
			m.ensureVisible()
			cmd := m.scheduleEff()
			return m, cmd
		}
	case "?":
		m.dlg = dlgHelp
	case "/":
		m.filtering = true
	case "j", "down":
		cmd := m.move(1)
		return m, cmd
	case "k", "up":
		cmd := m.move(-1)
		return m, cmd
	case "g", "home":
		cmd := m.move(-len(m.rows))
		return m, cmd
	case "G", "end":
		cmd := m.move(len(m.rows))
		return m, cmd
	case "pgdown", "ctrl+d":
		cmd := m.move(m.listHeight() / 2)
		return m, cmd
	case "pgup", "ctrl+u":
		cmd := m.move(-m.listHeight() / 2)
		return m, cmd
	case "x":
		if m.cfg != nil && !m.cfg.Missing && !m.consent && !m.editing {
			m.dlg = dlgConsent
		}
	case "m":
		m.startMove()
	case "n":
		m.startNote()
	case "c":
		if a := m.selectedAlias(); a != "" && m.cfg != nil && !m.cfg.Missing && !m.editing {
			c, err := m.connectCommand()
			if err != nil {
				m.setStatus(stErr, "cannot connect: "+err.Error())
				return m, nil
			}
			return m, tea.ExecProcess(c, func(err error) tea.Msg { return connectDoneMsg{a, err} })
		}
	case "space":
		m.toggleFold()
	case "enter", "e":
		if r, ok := m.current(); ok && r.head && k == "enter" {
			m.toggleFold()
			return m, nil
		}
		if m.editing { // one edit at a time; a refused start must not end the edit state of another
			return m, nil
		}
		cmd, starts := m.startEdit()
		if starts {
			m.resetEff() // the file can change now
			m.editing = true
		}
		return m, cmd
	}
	return m, nil
}

// connectCommand returns the ssh command for the selected host. It starts no
// shell. sshconfig.Args says when ssh gets -F.
func (m Model) connectCommand() (*exec.Cmd, error) {
	a := m.selectedAlias()
	if err := sshconfig.SafeAlias(a); err != nil {
		return nil, err
	}
	return exec.Command("ssh", sshconfig.Args(m.path, false, a)...), nil
}

func (m *Model) setStatus(kind int, s string) { m.status, m.statusKind = sanitize(s), kind }

// resetEff removes the consent and all `ssh -G` results. The seq change drops
// a queued effTickMsg. The new map drops a result that is still on its way.
func (m *Model) resetEff() {
	m.consent = false
	m.eff = map[string]*effState{}
	m.seq++
}

// startEdit returns the command for an edit. starts is true when the command
// makes a backup and opens the editor, and false when it only reports why not.
func (m Model) startEdit() (cmd tea.Cmd, starts bool) {
	if m.cfg == nil {
		return nil, false
	}
	if m.cfg.Missing {
		return func() tea.Msg { return prepMsg{err: fmt.Errorf("%s does not exist; create it first", tilde(m.path))} }, false
	}
	line := 0
	if r, ok := m.current(); ok {
		if b := r.block(); b != nil {
			if b.Included {
				p := tilde(b.File)
				return func() tea.Msg {
					return prepMsg{err: fmt.Errorf("defined in %s · this app edits only %s", p, tilde(m.path))}
				}, false
			}
			line = b.Line
		}
	}
	path := m.path
	return func() tea.Msg {
		s, err := edit.Prepare(path)
		return prepMsg{sess: s, line: line, err: err}
	}, true
}

// wrote sets the status after a write of the config. done says what the write
// did. It reports whether the file changed.
func (m *Model) wrote(done, backup string, err error) bool {
	switch {
	case errors.Is(err, sshedit.ErrStale):
		m.setStatus(stErr, "config changed on disk · nothing written · reloaded")
	case err != nil:
		m.setStatus(stErr, err.Error())
	case backup == "":
		m.setStatus(stInfo, "no changes")
	default:
		m.setStatus(stOK, done+" · backup: "+short(backup))
		return true
	}
	return false
}

// tagHost returns the selected host when the app can write a tag for it. If
// not, it sets a status that says why. what completes "select a host to".
// It reads no file.
func (m *Model) tagHost(what string) (*sshconfig.Host, bool) {
	if m.cfg == nil || m.editing {
		return nil, false
	}
	if m.cfg.Missing {
		m.setStatus(stErr, tilde(m.path)+" does not exist; create it first")
		return nil, false
	}
	r, ok := m.current()
	if !ok || r.host == nil {
		m.setStatus(stInfo, "select a host to "+what)
		return nil, false
	}
	if b := r.host.Block; b.Included {
		m.setStatus(stErr, fmt.Sprintf("defined in %s · this app edits only %s", tilde(b.File), tilde(m.path)))
		return nil, false
	}
	if !m.cfg.Hashed {
		m.setStatus(stErr, tilde(m.path)+" was not fully read; the app does not change it")
		return nil, false
	}
	return r.host, true
}

// startMove opens the move dialog for the selected host, or sets a status
// that says why not.
func (m *Model) startMove() {
	h, ok := m.tagHost("move it to a group")
	if !ok {
		return
	}
	b := h.Block
	mv := moveState{alias: h.Alias, line: b.Line, patterns: b.Patterns, from: b.Group}
	for _, g := range m.cfg.Groups() {
		if g == b.Group {
			mv.cur = len(mv.items)
		}
		mv.items = append(mv.items, moveItem{mvGroup, g})
	}
	if b.Group == "" {
		mv.cur = len(mv.items)
	}
	mv.items = append(mv.items, moveItem{kind: mvNone}, moveItem{kind: mvNew})
	m.mv, m.dlg = mv, dlgMove
}

func (m Model) moveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mv := &m.mv
	k := msg.String()
	if mv.typing {
		switch k {
		case "esc":
			mv.typing, mv.name, mv.err = false, "", ""
		case "enter":
			name := strings.TrimSpace(mv.name)
			if err := sshconfig.ValidGroupName(name); err != nil {
				mv.err = err.Error()
				return m, nil
			}
			return m.confirmMove(name)
		case "backspace":
			if rs := []rune(mv.name); len(rs) > 0 {
				mv.name, mv.err = string(rs[:len(rs)-1]), ""
			}
		default:
			if msg.Text != "" {
				mv.name, mv.err = mv.name+msg.Text, ""
			}
		}
		return m, nil
	}
	switch k {
	case "esc", "q":
		m.dlg = dlgNone
	case "up", "k", "ctrl+p":
		mv.cur = max(0, mv.cur-1)
	case "down", "j", "ctrl+n":
		mv.cur = min(len(mv.items)-1, mv.cur+1)
	case "enter":
		if it := mv.items[mv.cur]; it.kind != mvNew {
			return m.confirmMove(it.name)
		}
		mv.typing = true
	}
	return m, nil
}

// confirmMove closes the dialog and returns the command that writes the
// group. From here to the next load the app is in the edit state, as for an
// editor session: the file can change, so the consent ends.
func (m Model) confirmMove(group string) (tea.Model, tea.Cmd) {
	m.dlg = dlgNone
	mv := m.mv
	if group == mv.from {
		m.setStatus(stInfo, fmt.Sprintf("%s is already in %s", mv.alias, groupLabel(group)))
		return m, nil
	}
	m.resetEff()
	m.editing = true
	path, hash := m.path, m.cfg.Hash
	return m, func() tea.Msg {
		bak, err := sshedit.SetGroup(path, hash, mv.line, mv.patterns, group)
		return movedMsg{alias: mv.alias, group: group, backup: bak, err: err}
	}
}

// toggleFold folds or unfolds the group of the selected header. A search
// shows all matches, so it does nothing then.
func (m *Model) toggleFold() {
	r, ok := m.current()
	if !ok || !r.head || m.query != "" {
		return
	}
	m.folded[r.group] = !m.folded[r.group]
	m.rebuild()
	m.reselect(rowID{true, r.group})
	m.ensureVisible()
}

// reselect moves the cursor to the row with this id, if it is visible.
func (m *Model) reselect(id rowID) {
	for i, r := range m.rows {
		if rid, ok := r.id(); ok && rid == id {
			m.cursor = i
		}
	}
}

// rebuild recomputes the visible rows from the config and the query. Hosts
// stay in file order. When at least one host has a group, each group gets a
// header row, in first-seen order, and the hosts without a group come last.
func (m *Model) rebuild() {
	m.rows, m.nTop, m.nHosts = nil, 0, 0
	if m.cfg == nil {
		return
	}
	type scored struct {
		r     row
		score int
	}
	var hs, rs []scored
	for i := range m.cfg.Hosts {
		h := &m.cfg.Hosts[i]
		text := h.Alias + " " + h.Block.Get("HostName") + " " + h.Block.Get("User") + " " + h.Block.Group + " " + strings.Join(h.Block.Note, " ")
		if s := sshconfig.FuzzyScore(m.query, text); s > 0 {
			hs = append(hs, scored{row{host: h}, s})
		}
	}
	for _, b := range m.cfg.Rules {
		if s := sshconfig.FuzzyScore(m.query, strings.Join(b.Patterns, " ")); s > 0 {
			rs = append(rs, scored{row{rule: b}, s})
		}
	}
	if m.query != "" {
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].score > hs[j].score })
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].score > rs[j].score })
	}
	groups := m.cfg.Groups()
	if len(groups) == 0 {
		for _, s := range hs {
			m.rows = append(m.rows, s.r)
		}
	} else {
		by := map[string][]row{}
		for _, s := range hs {
			g := s.r.host.Block.Group
			by[g] = append(by[g], s.r)
		}
		for _, g := range append(groups, "") {
			if len(by[g]) == 0 {
				continue
			}
			m.rows = append(m.rows, row{head: true, group: g, count: len(by[g])})
			if m.query != "" || !m.folded[g] {
				m.rows = append(m.rows, by[g]...)
			}
		}
	}
	m.nTop, m.nHosts = len(m.rows), len(hs)
	for _, s := range rs {
		m.rows = append(m.rows, s.r)
	}
	m.cursor = 0
	m.offset = 0
}

func (m Model) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) selectedAlias() string {
	if r, ok := m.current(); ok && r.host != nil {
		return r.host.Alias
	}
	return ""
}

func (m *Model) move(d int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	m.cursor = max(0, min(len(m.rows)-1, m.cursor+d))
	m.ensureVisible()
	return m.scheduleEff()
}

// effAllowed reports whether `ssh -G` can run. It always needs consent,
// because the parser cannot prove that a config runs no commands.
func (m Model) effAllowed() bool { return m.cfg != nil && m.consent && !m.editing }

// scheduleEff starts a short timer before `ssh -G`, so fast scrolling does not
// start many processes.
func (m *Model) scheduleEff() tea.Cmd {
	a := m.selectedAlias()
	if a == "" || !m.effAllowed() || m.eff[a] != nil {
		return nil
	}
	m.seq++
	seq := m.seq
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return effTickMsg{seq, a} })
}

type spec struct {
	kind int // 0 header, 1 row, 2 blank
	row  int
	text string
}

func (m Model) listSpecs() []spec {
	var s []spec
	if m.nTop > 0 {
		s = append(s, spec{0, -1, fmt.Sprintf("HOSTS · %d", m.nHosts)})
		for i := 0; i < m.nTop; i++ {
			s = append(s, spec{1, i, ""})
		}
	}
	if n := len(m.rows) - m.nTop; n > 0 {
		if m.nTop > 0 {
			s = append(s, spec{2, -1, ""})
		}
		s = append(s, spec{0, -1, fmt.Sprintf("RULES · %d", n)})
		for i := m.nTop; i < len(m.rows); i++ {
			s = append(s, spec{1, i, ""})
		}
	}
	return s
}

func (m Model) bodyHeight() int { return max(3, m.h-3) }

func (m Model) listHeight() int { return max(1, m.bodyHeight()-2-2) }

func (m *Model) ensureVisible() {
	specs := m.listSpecs()
	lh := m.listHeight()
	for i, s := range specs {
		if s.kind == 1 && s.row == m.cursor {
			if i-1 >= 0 && specs[i-1].kind == 0 && i-1 < m.offset {
				m.offset = i - 1
			}
			if i < m.offset {
				m.offset = i
			}
			if i >= m.offset+lh {
				m.offset = i - lh + 1
			}
		}
	}
	m.offset = max(0, min(m.offset, max(0, len(specs)-lh)))
}

func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && (p == h || strings.HasPrefix(p, h+string(filepath.Separator))) {
		return "~" + p[len(h):]
	}
	return p
}

func short(p string) string { return filepath.Base(p) }
