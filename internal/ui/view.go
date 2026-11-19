package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dansku/sshwave/internal/sshconfig"
)

// View renders the screen.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.BackgroundColor = cBg
	v.ForegroundColor = cText
	v.WindowTitle = "sshwave"
	return v
}

func (m Model) render() string {
	if m.w < 40 || m.h < 10 {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center,
			sMuted.Render("terminal too small\n"+fmt.Sprintf("%dx%d · need 40x10", m.w, m.h)))
	}
	bodyH := m.bodyHeight()
	var body string
	switch {
	case !m.loaded:
		body = lipgloss.Place(m.w, bodyH, lipgloss.Center, lipgloss.Center, sMuted.Render("loading…"))
	case m.cfg.Missing || (len(m.cfg.Hosts) == 0 && len(m.cfg.Rules) == 0):
		body = m.empty(bodyH)
	default:
		body = m.panes(bodyH)
	}
	switch m.dlg {
	case dlgHelp:
		body = lipgloss.Place(m.w, bodyH, lipgloss.Center, lipgloss.Center, m.helpDialog())
	case dlgConsent:
		body = lipgloss.Place(m.w, bodyH, lipgloss.Center, lipgloss.Center, m.consentDialog())
	case dlgMove:
		body = lipgloss.Place(m.w, bodyH, lipgloss.Center, lipgloss.Center, m.moveDialog(bodyH))
	case dlgNote:
		body = lipgloss.Place(m.w, bodyH, lipgloss.Center, lipgloss.Center, m.noteDialog(bodyH))
	}
	return strings.Join([]string{m.header(), sBorder.Render(strings.Repeat("─", m.w)), body, m.footer()}, "\n")
}

func (m Model) header() string {
	left := "  " + gradient("◈ SSHWAVE", cMagenta, cCyan)
	right := ""
	if m.cfg != nil && !m.cfg.Missing {
		right = sMuted.Render(fmt.Sprintf("%d hosts · %d rules", len(m.cfg.Hosts), len(m.cfg.Rules)))
		if n := len(m.cfg.Problems); n > sshconfig.MaxProblems { // the last entry says that there are more
			right += sWarn.Render(fmt.Sprintf("  ⚠ %d+ problems", sshconfig.MaxProblems))
		} else if n > 0 {
			right += sWarn.Render(fmt.Sprintf("  ⚠ %d problem", n) + plural(n))
		}
	}
	right += "  "
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (m Model) footer() string {
	var s string
	if m.status != "" {
		st := map[int]lipgloss.Style{stInfo: sMuted, stOK: fg(cGreen), stWarn: sWarn, stErr: fg(cRed)}[m.statusKind]
		s = " " + st.Render("● "+m.status)
	} else if m.filtering {
		s = hints([][2]string{{"enter", "accept"}, {"esc", "clear"}, {"↑↓", "move"}})
	} else {
		enter := "edit"
		if r, ok := m.current(); ok && r.head {
			enter = "fold"
		}
		items := [][2]string{{"↵", enter}}
		if m.selectedAlias() != "" {
			items = append(items, [2]string{"c", "connect"})
		}
		items = append(items, [2]string{"m", "group"}, [2]string{"n", "note"}, [2]string{"/", "search"})
		if m.cfg != nil && !m.cfg.Missing && !m.consent {
			items = append(items, [2]string{"x", "resolve"})
		}
		items = append(items, [2]string{"?", "help"}, [2]string{"q", "quit"})
		// The move keys are in the help. They show only when there is room.
		if all := hints(slices.Insert(slices.Clone(items), len(items)-2, [2]string{"j/k", "move"})); lipgloss.Width(all) <= m.w {
			s = all
		} else {
			s = hints(items)
		}
	}
	return fit(s, m.w)
}

func hints(items [][2]string) string {
	var parts []string
	for _, it := range items {
		parts = append(parts, sKey.Render(" "+it[0]+" ")+" "+sMuted.Render(it[1]))
	}
	return " " + strings.Join(parts, "  ")
}

func (m Model) panes(h int) string {
	if m.w < 72 {
		return m.listPanel(m.w, h)
	}
	lw := max(34, min(44, m.w/3))
	rw := m.w - lw - 1
	return lipgloss.JoinHorizontal(lipgloss.Top, m.listPanel(lw, h), " ", m.detailPanel(rw, h))
}

func (m Model) listPanel(w, h int) string {
	iw := w - 4
	search := sAccent.Render("/ ")
	switch {
	case m.query == "" && !m.filtering:
		search += sMuted.Render("Search hosts…")
	case m.filtering:
		search += sText.Render(sanitize(m.query)) + sCyan.Render("▏")
	default:
		search += sText.Render(sanitize(m.query))
	}
	lines := []string{fit(search, iw), sBorder.Render(strings.Repeat("─", iw))}

	specs := m.listSpecs()
	if len(specs) == 0 {
		lines = append(lines, sMuted.Render("No matches"))
	}
	lh := m.listHeight()
	end := min(len(specs), m.offset+lh)
	for _, sp := range specs[min(m.offset, len(specs)):end] {
		switch sp.kind {
		case 0:
			lines = append(lines, sMuted.Render(sp.text))
		case 1:
			lines = append(lines, m.rowLine(sp.row, iw))
		default:
			lines = append(lines, "")
		}
	}
	title := "Hosts"
	return panel(title, lines, w, h, true)
}

func (m Model) rowLine(i, iw int) string {
	r := m.rows[i]
	sel := i == m.cursor
	bg := lipgloss.NewStyle()
	if sel {
		bg = bg.Background(cPanel)
	}
	var name, mark, right string
	var nameSt lipgloss.Style
	if r.head {
		icon := "▾ "
		if m.folded[r.group] && m.query == "" {
			icon = "▸ "
		}
		name = fmt.Sprintf("%s%s · %d", icon, groupLabel(r.group), r.count)
		nameSt = bg.Foreground(cCyan)
	} else if r.host != nil {
		name = sanitize(r.host.Alias)
		right = sanitize(r.host.Block.Get("HostName"))
		nameSt = bg.Foreground(cText)
		if r.host.Block.Included {
			right = "inc"
		}
		if len(r.host.Block.Note) > 0 {
			mark = " " + noteMark
		}
	} else {
		icon := "✱ "
		if r.rule.Kind == "match" {
			icon = "◇ "
		}
		name = icon + sanitize(strings.Join(r.rule.Patterns, " "))
		nameSt = bg.Foreground(cMuted)
	}
	if sel {
		nameSt = nameSt.Bold(true)
	}
	marker := bg.Render(" ")
	if sel {
		marker = bg.Foreground(cMagenta).Render("▌")
	}
	avail := iw - 2
	rightW := 0
	if iw >= 28 && right != "" {
		rightW = min(lipgloss.Width(right), avail/2)
		right = ansi.Truncate(right, rightW, "…")
	} else {
		right = ""
	}
	nameW := avail - rightW - 1 - lipgloss.Width(mark) // the note mark stays when the name is cut
	name = ansi.Truncate(name, nameW, "…")
	pad := avail - lipgloss.Width(name) - lipgloss.Width(mark) - rightW
	return marker + bg.Render(" ") + nameSt.Render(name) + bg.Foreground(cCyan).Render(mark) + bg.Render(strings.Repeat(" ", max(0, pad))) +
		bg.Foreground(cMuted).Render(right)
}

func (m Model) detailPanel(w, h int) string {
	iw := w - 4
	ih := h - 2
	r, ok := m.current()
	if !ok {
		return panel("Details", []string{sMuted.Render("Nothing selected")}, w, h, false)
	}
	if r.head {
		lines := []string{
			sTitle.Render(groupLabel(r.group)) + "  " + lipgloss.NewStyle().Foreground(cBg).Background(cMuted).Render(" GROUP "),
			sMuted.Render(fmt.Sprintf("%d host", r.count) + plural(r.count)),
			"",
			sKey.Render(" ↵ ") + sMuted.Render(" fold or unfold"),
			sKey.Render(" m ") + sMuted.Render(" on a host moves it to a group"),
			sKey.Render(" n ") + sMuted.Render(" on a host edits its note"),
		}
		for i := range lines {
			lines[i] = fit(lines[i], iw)
		}
		return panel("Details", lines, w, h, true)
	}
	b := r.block()
	var lines []string
	title := strings.Join(b.Patterns, " ")
	if r.host != nil {
		title = r.host.Alias
	}
	head := sTitle.Render(sanitize(title))
	if b.Included {
		head += "  " + lipgloss.NewStyle().Foreground(cBg).Background(cOrange).Render(" INCLUDED ")
	}
	if r.rule != nil {
		head += "  " + lipgloss.NewStyle().Foreground(cBg).Background(cMuted).Render(" RULE ")
	}
	lines = append(lines, head, sMuted.Render(fmt.Sprintf("%s:%d", sanitize(tilde(b.File)), b.Line)))
	if r.host != nil && b.Group != "" {
		lines = append(lines, sMuted.Render("group: "+sanitize(b.Group)))
	}
	var note []string
	if r.host != nil {
		note = noteLines(b.Note, max(3, ih/5))
	}
	lines = append(lines, note...)
	lines = append(lines, "")

	lines = append(lines, label("CONFIGURATION", iw))
	maxRaw := max(4, ih/2-3-len(note)) // the note takes room from the block
	for i, raw := range b.Raw {
		if i >= maxRaw {
			lines = append(lines, sMuted.Render(fmt.Sprintf("… %d more lines", len(b.Raw)-maxRaw)))
			break
		}
		lines = append(lines, rawLine(raw, i == 0))
	}
	lines = append(lines, "")
	lines = append(lines, label("EFFECTIVE", iw))
	lines = append(lines, m.effectiveLines(r, iw)...)

	for i := range lines {
		lines[i] = fit(lines[i], iw)
	}
	return panel("Details", lines, w, h, true)
}

// noteMark shows that a host has a note.
const noteMark = "✎"

// noteLines returns the lines of a note for the detail panel, n at most. A
// longer note ends with a line that counts the rest.
func noteLines(note []string, n int) []string {
	var out []string
	for i, l := range note {
		if len(note) > n && i == n-1 {
			out = append(out, sMuted.Render(fmt.Sprintf("  … %d more", len(note)-i)))
			break
		}
		prefix := "  "
		if i == 0 {
			prefix = noteMark + " "
		}
		out = append(out, sCyan.Render(prefix+sanitize(l)))
	}
	return out
}

func label(s string, w int) string {
	return sAccent.Render(s) + " " + sBorder.Render(strings.Repeat("─", max(0, w-len(s)-1)))
}

func rawLine(raw string, first bool) string {
	// A tab becomes spaces, so the indent stays readable after sanitize.
	raw = sanitize(strings.ReplaceAll(raw, "\t", "    "))
	trim := strings.TrimLeft(raw, " ")
	indent := raw[:len(raw)-len(trim)]
	if strings.HasPrefix(trim, "#") {
		return indent + sMuted.Render(trim)
	}
	key, rest, _ := strings.Cut(trim, " ")
	if first {
		return indent + sAccent.Render(key) + " " + sText.Render(rest)
	}
	return indent + sCyan.Render(key) + " " + sText.Render(rest)
}

var effKeys = []string{"hostname", "user", "port", "identityfile", "proxyjump", "proxycommand",
	"forwardagent", "identitiesonly", "serveraliveinterval"}

func (m Model) effectiveLines(r row, iw int) []string {
	if r.host == nil {
		return []string{sMuted.Render("A rule changes settings for the hosts it matches.")}
	}
	if !m.consent {
		st, why, risk := m.consentWhy()
		return []string{st.Render(why), sMuted.Render(risk),
			sKey.Render(" x ") + sMuted.Render(" allow and resolve")}
	}
	st := m.eff[r.host.Alias]
	switch {
	case st == nil || st.loading:
		return []string{sMuted.Render("resolving…")}
	case st.err != nil:
		return []string{fg(cRed).Render(ansi.Truncate(sanitize(st.err.Error()), iw, "…"))}
	}
	var out []string
	for _, k := range effKeys {
		n := 0
		for _, p := range st.pairs {
			if p.Key != k {
				continue
			}
			n++
			if n == 3 {
				out = append(out, sMuted.Render(fmt.Sprintf("%-14s", "")+"+ more"))
				break
			}
			out = append(out, sMuted.Render(fmt.Sprintf("%-14s", k))+sText.Render(sanitize(p.Value)))
		}
	}
	return out
}

func (m Model) empty(h int) string {
	art := []string{
		"      ▄▄██████▄▄      ",
		"    ▄████████████▄    ",
		"   ████████████████   ",
		"  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀  ",
		"   ─ ─ ─ ─ ─ ─ ─ ─    ",
		"  ───────────────────  ",
	}
	cols := lipgloss.Blend1D(len(art), cOrange, cMagenta)
	var lines []string
	for i, l := range art {
		lines = append(lines, fg(cols[i]).Render(l))
	}
	lines = append(lines, "")
	if m.cfg.Missing {
		lines = append(lines, sBold.Render("No SSH config found"), sMuted.Render(sanitize(tilde(m.path))+" does not exist"))
	} else {
		lines = append(lines, sBold.Render("No hosts yet"), sMuted.Render("Press e to open "+sanitize(tilde(m.path))))
	}
	return lipgloss.Place(m.w, h, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, lines...))
}

func (m Model) helpDialog() string {
	rows := [][2]string{
		{"j/k ↑↓ g/G", "move / top / bottom"}, {"/", "search (fuzzy)"},
		{"enter / e", "edit ~/.ssh/config in $EDITOR"}, {"m", "move host to group"},
		{"n / c", "edit note / connect with ssh"},
		{"space", "fold / unfold a group"}, {"x", "allow ssh -G for this load"},
		{"esc", "clear search / close"}, {"? / q", "this help / quit"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, sKey.Render(fmt.Sprintf(" %-11s", r[0]))+" "+sText.Render(r[1]))
	}
	lines = append(lines, "", sMuted.Render("A backup is saved before every edit."))
	return panel("Keys", lines, 52, len(lines)+2, true)
}

// consentWhy returns the style and the two lines that say why `ssh -G` needs
// consent. The warning is stronger when the parser found a reason.
func (m Model) consentWhy() (st lipgloss.Style, why, risk string) {
	switch {
	case m.cfg.MatchExec:
		return sWarn, "This config has Match exec.", "ssh -G will run those local commands."
	case m.cfg.Untrusted:
		return sWarn, "This config was not fully read.", "ssh -G may run local commands."
	}
	return sMuted, "Settings are not resolved yet.", "ssh -G may run local commands."
}

func (m Model) consentDialog() string {
	st, why, risk := m.consentWhy()
	lines := []string{
		st.Render(why),
		sText.Render(risk),
		sText.Render("The app asks again after each reload."),
		"",
		sKey.Render(" y ") + sMuted.Render(" allow    ") + sKey.Render(" n ") + sMuted.Render(" cancel"),
	}
	return panel("Run ssh -G?", lines, min(m.w-4, 48), len(lines)+2, true)
}

// moveDialog lists the groups that the host can move to. With the name input
// open, it shows the input and the reason why a name is refused.
func (m Model) moveDialog(bodyH int) string {
	mv := m.mv
	w := min(m.w-4, 48)
	iw := w - 4
	lines := []string{sText.Render(sanitize(mv.alias)), ""}
	if mv.typing {
		lines = append(lines, sMuted.Render("New group: ")+sText.Render(sanitize(mv.name))+sCyan.Render("▏"))
		if mv.err != "" {
			lines = append(lines, fg(cRed).Render(sanitize(mv.err)))
		} else {
			lines = append(lines, "")
		}
		lines = append(lines, "", sKey.Render(" ↵ ")+sMuted.Render(" move    ")+sKey.Render(" esc ")+sMuted.Render(" back"))
		return panel("Move to group", lines, w, len(lines)+2, true)
	}
	// A window of the items around the cursor, so a long list fits.
	n := max(1, min(len(mv.items), bodyH-6))
	first := max(0, min(mv.cur-n/2, len(mv.items)-n))
	for i := first; i < first+n; i++ {
		it := mv.items[i]
		label := groupLabel(it.name)
		if it.kind == mvNew {
			label = "New group…"
		}
		if it.kind != mvNew && it.name == mv.from {
			label += "  (current)"
		}
		st, marker := sText, "  "
		if it.kind != mvGroup {
			st = sMuted
		}
		if i == mv.cur {
			st, marker = st.Background(cPanel).Bold(true), fg(cMagenta).Background(cPanel).Render("▌")+lipgloss.NewStyle().Background(cPanel).Render(" ")
		}
		lines = append(lines, marker+st.Render(fit(label, iw-2)))
	}
	lines = append(lines, "", sKey.Render(" ↵ ")+sMuted.Render(" move    ")+sKey.Render(" esc ")+sMuted.Render(" cancel"))
	return panel("Move to group", lines, w, len(lines)+2, true)
}

// noteDialog shows the note editor: the text around the cursor, the line
// count or the reason why the note is refused, and the keys.
func (m Model) noteDialog(bodyH int) string {
	nt := m.nt
	w := min(m.w-4, 60)
	iw := w - 4
	roomy := bodyH >= 12
	n := max(1, min(sshconfig.MaxNoteLines, bodyH-6))
	if roomy {
		n = min(sshconfig.MaxNoteLines, bodyH-8)
	}
	lines := []string{sText.Render(sanitize(nt.alias))}
	if roomy {
		lines = append(lines, "")
	}
	// A window of the lines around the cursor, so a long text fits.
	first := max(0, min(nt.row-n/2, len(nt.lines)-n))
	for i := first; i < first+n; i++ {
		switch {
		case i >= len(nt.lines):
			lines = append(lines, "")
		case i != nt.row:
			lines = append(lines, sText.Render(sanitize(nt.lines[i])))
		default:
			rs := []rune(nt.lines[i])
			pre, post := sanitize(string(rs[:nt.col])), sanitize(string(rs[nt.col:]))
			if over := ansi.StringWidth(pre) - (iw - 2); over > 0 { // keep the cursor in view
				pre = ansi.TruncateLeft(pre, over+1, "…")
			}
			lines = append(lines, sText.Render(pre)+sCyan.Render("▏")+sText.Render(post))
		}
	}
	if roomy {
		lines = append(lines, "")
	}
	if p := nt.problem(); p != "" {
		lines = append(lines, fg(cRed).Render(sanitize(p)))
	} else {
		note, _ := sshconfig.CleanNote(nt.lines)
		lines = append(lines, sMuted.Render(fmt.Sprintf("%d/%d lines · line %d: %d/%d bytes", len(note), sshconfig.MaxNoteLines,
			nt.row+1, len(strings.Trim(nt.lines[nt.row], " ")), sshconfig.MaxNoteLine)))
	}
	lines = append(lines,
		sWarn.Render("Plain text in your config. Do not store passwords."),
		sKey.Render(" ^S ")+sMuted.Render(" save   ")+sKey.Render(" ↵ ")+sMuted.Render(" new line   ")+sKey.Render(" esc ")+sMuted.Render(" cancel"))
	return panel("Note", lines, w, len(lines)+2, true)
}

// panel draws a rounded box with the title in the top border.
func panel(title string, lines []string, w, h int, focus bool) string {
	bs := sBorder
	if focus {
		bs = fg(cMagenta)
	}
	iw := w - 4
	t := " " + title + " "
	top := bs.Render("╭─") + sBold.Render(t) + bs.Render(strings.Repeat("─", max(0, w-3-lipgloss.Width(t)))+"╮")
	out := []string{top}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, bs.Render("│")+" "+fit(l, iw)+" "+bs.Render("│"))
	}
	out = append(out, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

// fit cuts s to w cells and pads it with spaces to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}
