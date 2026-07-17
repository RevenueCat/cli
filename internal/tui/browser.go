package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// BrowserItem is one row in a list/table view or the subject of a detail view.
type BrowserItem struct {
	ID    string
	Label string   // primary text in simple-list and breadcrumb
	Meta  string   // dim secondary text in simple-list
	Row   []string // cells used when the parent frame is in table mode

	WebURL string
	Fields []BrowserField
	Links  []BrowserLink

	// OpenChart: if set, Enter fetches and embeds the chart directly in the
	// browser (no separate program). Takes priority over DirectLoad.
	OpenChart func() (tea.Model, error)

	// DirectLoad bypasses the detail view: pressing Enter fires the func and
	// pushes a new frame. Return non-nil cols to get a table frame.
	DirectLoad func() (title string, cols []string, items []BrowserItem, err error)

	// AutoLoad fires when the detail view opens; results are inline tables.
	AutoLoad func() (sections []BrowserSection, err error)
}

// BrowserField is a key/value row in a detail view.
type BrowserField struct {
	Key   string
	Value string
}

// BrowserLink is a nav entry in the detail view that pushes a child list.
type BrowserLink struct {
	Label string
	Load  func() (title string, items []BrowserItem, err error)
}

// BrowserSection is an inline table in a detail view, populated by AutoLoad.
type BrowserSection struct {
	Title string
	Cols  []string
	Rows  []BrowserSectionRow
	Empty string
}

// BrowserSectionRow is one row in a BrowserSection.
// If Item is non-nil the row is selectable.
type BrowserSectionRow struct {
	Cells []string
	Item  *BrowserItem
}

// RunBrowser launches a simple-list browser frame.
func RunBrowser(title string, items []BrowserItem) error {
	return run(newListFrame(title, items))
}

// RunBrowserTable launches a table browser frame.
func RunBrowserTable(title string, cols []string, items []BrowserItem) error {
	return run(newTableFrame(title, cols, items))
}

func run(initial bframe) error {
	if !IsInteractive() {
		return fmt.Errorf("interactive browser requires a terminal; use --json or --no-input variants instead")
	}
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	m := &browser{stack: []bframe{initial}, width: w, height: h}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// OpenURL opens url in the default system browser.
func OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		// The first quoted argument to `start` is the window title; pass an
		// empty title so URLs with special characters open correctly.
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ── frames ───────────────────────────────────────────────────────────────────

type frameKind int

const (
	kindList frameKind = iota
	kindTable
	kindDetail
)

type bframe struct {
	kind frameKind

	// list / table state
	title     string
	all       []BrowserItem
	filter    string
	filtering bool
	cursor    int

	// table-only: column definitions
	tableCols []string

	// detail state
	item         BrowserItem
	detailCursor int
	sections     []BrowserSection
	autoLoading  bool
	autoErr      string
}

func newListFrame(title string, items []BrowserItem) bframe {
	return bframe{kind: kindList, title: title, all: items}
}

func newTableFrame(title string, cols []string, items []BrowserItem) bframe {
	return bframe{kind: kindTable, title: title, all: items, tableCols: cols}
}

func newDetailFrame(item BrowserItem) bframe {
	return bframe{
		kind:        kindDetail,
		item:        item,
		autoLoading: item.AutoLoad != nil,
	}
}

func (f *bframe) visible() []BrowserItem {
	if f.filter == "" {
		return f.all
	}
	q := strings.ToLower(f.filter)
	var out []BrowserItem
	for _, it := range f.all {
		if f.tableCols != nil {
			for _, cell := range it.Row {
				if strings.Contains(strings.ToLower(cell), q) {
					out = append(out, it)
					break
				}
			}
		} else {
			if strings.Contains(strings.ToLower(it.Label), q) ||
				strings.Contains(strings.ToLower(it.Meta), q) {
				out = append(out, it)
			}
		}
	}
	return out
}

// ── selectable slots ─────────────────────────────────────────────────────────

type slotKind int

const (
	slotLink slotKind = iota
	slotSectionRow
)

type detailSlot struct {
	kind slotKind
	link int
	sec  int
	row  int
}

func detailSlots(f *bframe) []detailSlot {
	var slots []detailSlot
	for i := range f.item.Links {
		slots = append(slots, detailSlot{kind: slotLink, link: i})
	}
	for si, sec := range f.sections {
		for ri, row := range sec.Rows {
			if row.Item != nil {
				slots = append(slots, detailSlot{kind: slotSectionRow, sec: si, row: ri})
			}
		}
	}
	return slots
}

// ── model ────────────────────────────────────────────────────────────────────

type childLoadedMsg struct {
	title string
	cols  []string // non-nil → table frame
	items []BrowserItem
	err   error
}

// chartEmbed is implemented by models that can be embedded in the browser.
// Done returns true when the model wants to close (user pressed q/esc).
type chartEmbed interface {
	tea.Model
	Done() bool
}

type chartReadyMsg struct {
	chart chartEmbed
	err   error
}

type autoLoadedMsg struct {
	frameIdx int
	sections []BrowserSection
	err      error
}

type browser struct {
	stack    []bframe
	width    int
	height   int
	loading  bool
	loadErr  string
	subChart chartEmbed // non-nil when a chart is embedded
}

func (m *browser) top() *bframe { return &m.stack[len(m.stack)-1] }

func (m *browser) Init() tea.Cmd { return nil }

func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// When a chart is embedded, delegate all messages to it.
	if m.subChart != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			m.width = ws.Width
			m.height = ws.Height
		}
		newModel, cmd := m.subChart.Update(msg)
		ce, ok := newModel.(chartEmbed)
		if !ok {
			// Should never happen; recover gracefully.
			m.subChart = nil
			m.loadErr = "chart model lost its interface (this is a bug)"
			return m, nil
		}
		m.subChart = ce
		if m.subChart.Done() {
			m.subChart = nil
			return m, nil
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case chartReadyMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err.Error()
		} else {
			m.subChart = msg.chart
			// Prime the chart with the current terminal size before first View().
			sized, _ := m.subChart.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			if ce, ok := sized.(chartEmbed); ok {
				m.subChart = ce
			}
			return m, m.subChart.Init()
		}
	case childLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err.Error()
		} else {
			m.loadErr = ""
			if len(msg.cols) > 0 {
				m.stack = append(m.stack, newTableFrame(msg.title, msg.cols, msg.items))
			} else {
				m.stack = append(m.stack, newListFrame(msg.title, msg.items))
			}
		}
	case autoLoadedMsg:
		if msg.frameIdx < len(m.stack) {
			f := &m.stack[msg.frameIdx]
			if f.kind != kindDetail {
				break // frame was replaced before async completed; discard
			}
			f.autoLoading = false
			if msg.err != nil {
				f.autoErr = msg.err.Error()
			} else {
				f.sections = msg.sections
			}
			// Clamp cursor so it stays within the new slot list.
			slots := detailSlots(f)
			if f.detailCursor >= len(slots) {
				f.detailCursor = brMax(len(slots)-1, 0)
			}
		}
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *browser) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loadErr != "" {
		m.loadErr = ""
		return m, nil
	}
	f := m.top()
	switch f.kind {
	case kindList, kindTable:
		return m.handleListKey(f, msg)
	case kindDetail:
		return m.handleDetailKey(f, msg)
	}
	return m, nil
}

func (m *browser) handleListKey(f *bframe, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if f.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			f.filtering = false
			f.filter = ""
			f.cursor = 0
		case tea.KeyBackspace:
			if len(f.filter) > 0 {
				f.filter = f.filter[:len(f.filter)-1]
				f.cursor = 0
			}
		case tea.KeyEnter:
			f.filtering = false
		case tea.KeyRunes:
			f.filter += string(msg.Runes)
			f.cursor = 0
		}
		return m, nil
	}

	v := f.visible()
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		} else {
			return m, tea.Quit
		}
	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
		}
	case "down", "j":
		if f.cursor < len(v)-1 {
			f.cursor++
		}
	case "/":
		f.filtering = true
	case "enter":
		if f.cursor < len(v) {
			item := v[f.cursor]
			if item.OpenChart != nil {
				m.loading = true
				fn := item.OpenChart
				return m, func() tea.Msg {
					model, err := fn()
					if err != nil {
						return chartReadyMsg{err: err}
					}
					ce, ok := model.(chartEmbed)
					if !ok {
						return chartReadyMsg{err: fmt.Errorf("chart model does not implement chartEmbed")}
					}
					return chartReadyMsg{chart: ce}
				}
			}
			if item.DirectLoad != nil {
				m.loading = true
				fn := item.DirectLoad
				return m, func() tea.Msg {
					title, cols, items, err := fn()
					return childLoadedMsg{title: title, cols: cols, items: items, err: err}
				}
			}
			frame := newDetailFrame(item)
			m.stack = append(m.stack, frame)
			if item.AutoLoad != nil {
				idx := len(m.stack) - 1
				fn := item.AutoLoad
				return m, func() tea.Msg {
					sections, err := fn()
					return autoLoadedMsg{frameIdx: idx, sections: sections, err: err}
				}
			}
		}
	case "o":
		if f.cursor < len(v) && v[f.cursor].WebURL != "" {
			if err := OpenURL(v[f.cursor].WebURL); err != nil {
				m.loadErr = "could not open browser: " + err.Error()
			}
		}
	}
	return m, nil
}

func (m *browser) handleDetailKey(f *bframe, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	slots := detailSlots(f)
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.stack = m.stack[:len(m.stack)-1]
	case "up", "k":
		if f.detailCursor > 0 {
			f.detailCursor--
		}
	case "down", "j":
		if f.detailCursor < len(slots)-1 {
			f.detailCursor++
		}
	case "enter":
		if len(slots) == 0 || f.detailCursor >= len(slots) {
			break
		}
		slot := slots[f.detailCursor]
		switch slot.kind {
		case slotLink:
			link := f.item.Links[slot.link]
			m.loading = true
			return m, func() tea.Msg {
				title, items, err := link.Load()
				return childLoadedMsg{title: title, items: items, err: err}
			}
		case slotSectionRow:
			item := f.sections[slot.sec].Rows[slot.row].Item
			if item == nil {
				break
			}
			frame := newDetailFrame(*item)
			m.stack = append(m.stack, frame)
			if item.AutoLoad != nil {
				idx := len(m.stack) - 1
				fn := item.AutoLoad
				return m, func() tea.Msg {
					sections, err := fn()
					return autoLoadedMsg{frameIdx: idx, sections: sections, err: err}
				}
			}
		}
	case "o":
		if f.item.WebURL != "" {
			if err := OpenURL(f.item.WebURL); err != nil {
				m.loadErr = "could not open browser: " + err.Error()
			}
		}
	}
	return m, nil
}

// ── view ─────────────────────────────────────────────────────────────────────

func (m *browser) View() string {
	if m.subChart != nil {
		return m.subChart.View()
	}
	if m.loading {
		return m.renderHeader("Loading…") + "\n  Loading data…\n"
	}
	if m.loadErr != "" {
		return m.renderHeader("Error") +
			"\n  " + brErr.Render("Error: "+m.loadErr) +
			"\n\n  Press any key to dismiss.\n"
	}
	f := m.top()
	switch f.kind {
	case kindList:
		return m.viewList(f)
	case kindTable:
		return m.viewTable(f)
	case kindDetail:
		return m.viewDetail(f)
	}
	return ""
}

func (m *browser) renderHeader(current string) string {
	var crumbs []string
	for i := 0; i < len(m.stack)-1; i++ {
		f := m.stack[i]
		switch f.kind {
		case kindList, kindTable:
			crumbs = append(crumbs, f.title)
		case kindDetail:
			lbl := f.item.ID
			if lbl == "" {
				lbl = f.item.Label
			}
			crumbs = append(crumbs, lbl)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n  ")
	if len(crumbs) > 0 {
		sb.WriteString(brDim.Render(strings.Join(crumbs, " › ") + " › "))
	}
	sb.WriteString(brTitle.Render(current))
	sb.WriteString("\n  ")
	sb.WriteString(brDim.Render(strings.Repeat("─", brMax(m.width-4, 10))))
	sb.WriteString("\n")
	return sb.String()
}

func (m *browser) filterBar(f *bframe) string {
	if f.filtering {
		return "  / " + brFilter.Render(f.filter) + "█\n\n"
	}
	if f.filter != "" {
		return "  / " + brDim.Render(f.filter) + "  " + brDim.Render("(esc to clear)") + "\n\n"
	}
	return "  " + brDim.Render("press / to filter") + "\n\n"
}

// scrollWindow returns the [start, end) slice of a list that fits on screen.
// overhead is the number of terminal lines consumed by non-list chrome
// (header + filter bar + column header if any + footer).
func (m *browser) scrollWindow(total, cursor, overhead int) (start, end int) {
	listH := m.height - overhead
	if listH < 3 {
		listH = 3
	}
	if cursor >= listH {
		start = cursor - listH + 1
	}
	end = start + listH
	if end > total {
		end = total
	}
	return start, end
}

// listOverhead is the number of fixed chrome lines in a simple-list view:
//
//	header(3) + filterBar(2) + footer-separator(2) + footer-hints(1) = 8
const listOverhead = 8

// tableOverhead adds the column-header row + blank line to listOverhead.
const tableOverhead = listOverhead + 2

// ── simple list view ─────────────────────────────────────────────────────────

func (m *browser) viewList(f *bframe) string {
	var sb strings.Builder
	v := f.visible()

	title := f.title
	if len(v) != len(f.all) {
		title += fmt.Sprintf(" (%d/%d)", len(v), len(f.all))
	} else if len(f.all) > 0 {
		title += fmt.Sprintf(" (%d)", len(f.all))
	}
	sb.WriteString(m.renderHeader(title))
	sb.WriteString(m.filterBar(f))

	start, end := m.scrollWindow(len(v), f.cursor, listOverhead)

	if len(v) == 0 {
		sb.WriteString("  " + brDim.Render("no results") + "\n")
	}
	for i := start; i < end; i++ {
		it := v[i]
		metaMax := 40
		labelMax := brMax(m.width-6-metaMax, 20)
		label := brTrunc(it.Label, labelMax)
		meta := brTrunc(it.Meta, metaMax)
		if i == f.cursor {
			sb.WriteString("  " + brCursor.Render("▶") + " " + brSelected.Render(label))
			if meta != "" {
				sb.WriteString("  " + brDim.Render(meta))
			}
		} else {
			sb.WriteString("    " + label)
			if meta != "" {
				sb.WriteString("  " + brDim.Render(meta))
			}
		}
		sb.WriteString("\n")
	}
	if len(v) > end-start {
		sb.WriteString("\n  " + brDim.Render(fmt.Sprintf("%d–%d of %d", start+1, end, len(v))) + "\n")
	}

	sb.WriteString("\n  " + brDim.Render(strings.Repeat("─", brMax(m.width-4, 10))) + "\n")
	sb.WriteString("  " + brDim.Render(m.listHints(f, v)) + "\n")
	return sb.String()
}

func (m *browser) listHints(f *bframe, v []BrowserItem) string {
	if f.filtering {
		return "type to filter  ·  esc cancel  ·  enter done"
	}
	parts := []string{"↑↓ move", "enter open", "/ filter"}
	if f.cursor < len(v) && v[f.cursor].WebURL != "" {
		parts = append(parts, "o web")
	}
	if len(m.stack) > 1 {
		parts = append(parts, "esc back")
	}
	parts = append(parts, "q quit")
	return strings.Join(parts, "  ·  ")
}

// ── table view ───────────────────────────────────────────────────────────────

func (m *browser) viewTable(f *bframe) string {
	var sb strings.Builder
	v := f.visible()

	title := f.title
	if len(v) != len(f.all) {
		title += fmt.Sprintf(" (%d/%d)", len(v), len(f.all))
	} else if len(f.all) > 0 {
		title += fmt.Sprintf(" (%d)", len(f.all))
	}
	sb.WriteString(m.renderHeader(title))
	sb.WriteString(m.filterBar(f))

	// Compute column widths from ALL items (prevents shifting during filter).
	colW := tableColWidths(f.tableCols, f.all, m.width-6)

	// Column header
	sb.WriteString("  " + brDim.Render("  ") + " ") // align with "    " prefix
	var hdr strings.Builder
	for i, c := range f.tableCols {
		if i > 0 {
			hdr.WriteString("  ")
		}
		hdr.WriteString(brPadRight(c, colW[i]))
	}
	sb.WriteString(brSection.Render(hdr.String()) + "\n\n")

	start, end := m.scrollWindow(len(v), f.cursor, tableOverhead)

	if len(v) == 0 {
		sb.WriteString("  " + brDim.Render("no results") + "\n")
	}
	for i := start; i < end; i++ {
		it := v[i]
		cells := it.Row
		if len(cells) == 0 {
			cells = []string{it.Label, it.Meta}
		}
		var row strings.Builder
		for j, cell := range cells {
			if j >= len(colW) {
				break
			}
			if j > 0 {
				row.WriteString("  ")
			}
			row.WriteString(brTrunc(brPadRight(cell, colW[j]), colW[j]))
		}
		rowStr := row.String()
		if i == f.cursor {
			sb.WriteString("  " + brCursor.Render("▶") + " " + brSelected.Render(rowStr) + "\n")
		} else {
			sb.WriteString("    " + rowStr + "\n")
		}
	}
	if len(v) > end-start {
		sb.WriteString("\n  " + brDim.Render(fmt.Sprintf("%d–%d of %d", start+1, end, len(v))) + "\n")
	}

	sb.WriteString("\n  " + brDim.Render(strings.Repeat("─", brMax(m.width-4, 10))) + "\n")
	sb.WriteString("  " + brDim.Render(m.listHints(f, v)) + "\n")
	return sb.String()
}

// tableColWidths computes column widths from headers + all items, then scales
// them proportionally if the total would exceed available terminal width.
func tableColWidths(cols []string, items []BrowserItem, available int) []int {
	colW := make([]int, len(cols))
	for i, c := range cols {
		colW[i] = len(c)
	}
	for _, it := range items {
		for i, cell := range it.Row {
			if i < len(colW) && len(cell) > colW[i] {
				colW[i] = len(cell)
			}
		}
	}
	// Fit to available width.
	sep := 2 * (len(colW) - 1)
	total := sep
	for _, w := range colW {
		total += w
	}
	if total > available {
		budget := available - sep
		if budget < len(colW)*6 {
			budget = len(colW) * 6
		}
		for i, w := range colW {
			scaled := budget * w / (total - sep)
			if scaled < 6 {
				scaled = 6
			}
			colW[i] = scaled
		}
	}
	return colW
}

// sectionColWidths computes column widths for a BrowserSection's inline table.
func sectionColWidths(sec BrowserSection, available int) []int {
	items := make([]BrowserItem, len(sec.Rows))
	for i, r := range sec.Rows {
		items[i] = BrowserItem{Row: r.Cells}
	}
	return tableColWidths(sec.Cols, items, available)
}

// ── detail view ──────────────────────────────────────────────────────────────

func (m *browser) viewDetail(f *bframe) string {
	var sb strings.Builder
	slots := detailSlots(f)

	label := f.item.Label
	if f.item.ID != "" && f.item.ID != f.item.Label {
		label = f.item.ID
	}
	sb.WriteString(m.renderHeader(label))
	sb.WriteString("\n")

	keyW := 0
	for _, field := range f.item.Fields {
		if field.Value != "" && len(field.Key) > keyW {
			keyW = len(field.Key)
		}
	}
	for _, field := range f.item.Fields {
		if field.Value == "" {
			continue
		}
		k := brPadRight(field.Key, keyW)
		sb.WriteString("  " + brDim.Render(k) + "  " + field.Value + "\n")
	}

	slotIdx := 0

	if len(f.item.Links) > 0 {
		sb.WriteString("\n  " + brSection.Render("Navigate to") + "\n")
		for _, link := range f.item.Links {
			if slotIdx == f.detailCursor {
				sb.WriteString("  " + brCursor.Render("▶") + " " + brLinkSel.Render(link.Label) + "\n")
			} else {
				sb.WriteString("    " + brLink.Render(link.Label) + "\n")
			}
			slotIdx++
		}
	}

	if f.autoLoading {
		sb.WriteString("\n  " + brDim.Render("Loading…") + "\n")
	} else if f.autoErr != "" {
		sb.WriteString("\n  " + brErr.Render("Error: "+f.autoErr) + "\n")
	} else {
		for _, sec := range f.sections {
			sb.WriteString("\n  " + brSection.Render(sec.Title) + "\n")
			if len(sec.Rows) == 0 {
				empty := sec.Empty
				if empty == "" {
					empty = "none"
				}
				sb.WriteString("  " + brDim.Render(empty) + "\n")
				continue
			}

			colW := sectionColWidths(sec, m.width-6)

			var hdr strings.Builder
			for i, c := range sec.Cols {
				if i > 0 {
					hdr.WriteString("  ")
				}
				hdr.WriteString(brPadRight(c, colW[i]))
			}
			sb.WriteString("  " + brDim.Render(hdr.String()) + "\n")

			for _, row := range sec.Rows {
				isSelectable := row.Item != nil
				isSel := isSelectable && slotIdx == f.detailCursor

				var cells strings.Builder
				for i, cell := range row.Cells {
					if i >= len(colW) {
						break
					}
					if i > 0 {
						cells.WriteString("  ")
					}
					cells.WriteString(brTrunc(brPadRight(cell, colW[i]), colW[i]))
				}
				cellStr := cells.String()

				switch {
				case isSel:
					sb.WriteString("  " + brCursor.Render("▶") + " " + brSelected.Render(cellStr) + "\n")
				case isSelectable:
					sb.WriteString("    " + cellStr + "\n")
				default:
					sb.WriteString("    " + brDim.Render(cellStr) + "\n")
				}

				if isSelectable {
					slotIdx++
				}
			}
		}
	}

	var hints []string
	if len(slots) > 0 {
		hints = append(hints, "↑↓ move", "enter open")
	}
	if f.item.WebURL != "" {
		hints = append(hints, "o web")
	}
	hints = append(hints, "esc back", "q quit")

	sb.WriteString("\n  " + brDim.Render(strings.Repeat("─", brMax(m.width-4, 10))) + "\n")
	sb.WriteString("  " + brDim.Render(strings.Join(hints, "  ·  ")) + "\n")
	return sb.String()
}

// ── styles ───────────────────────────────────────────────────────────────────

var (
	brTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	brCursor   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	brSelected = lipgloss.NewStyle().Bold(true)
	brDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	brFilter   = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	brSection  = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	brErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	brLink     = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	brLinkSel  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
)

// ── helpers ──────────────────────────────────────────────────────────────────

func brTrunc(s string, maxLen int) string {
	runes := []rune(s)
	if maxLen <= 3 || len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func brPadRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func brMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
