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

// BrowserItem is one row in a list view or the subject of a detail view.
type BrowserItem struct {
	ID     string
	Label  string         // primary text shown in list
	Meta   string         // dim secondary text shown next to Label
	WebURL string         // opened by the 'o' key
	Fields []BrowserField // key-value pairs in the detail view
	Links  []BrowserLink  // child resources the user can drill into
}

// BrowserField is a key/value row shown in a detail view.
type BrowserField struct {
	Key   string
	Value string
}

// BrowserLink is a button in the detail view that loads a child list on demand.
type BrowserLink struct {
	Label string
	Load  func() (title string, items []BrowserItem, err error)
}

// RunBrowser launches the full-screen interactive browser.
// Returns once the user presses q / ctrl+c.
// Only call when IsInteractive() is true.
func RunBrowser(title string, items []BrowserItem) error {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	m := &browser{
		stack:  []bframe{newListFrame(title, items)},
		width:  w,
		height: h,
	}
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
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ── frames ──────────────────────────────────────────────────────────────────

type frameKind int

const (
	kindList frameKind = iota
	kindDetail
)

type bframe struct {
	kind frameKind

	// list state
	title     string
	all       []BrowserItem
	filter    string
	filtering bool
	cursor    int

	// detail state
	item    BrowserItem
	linkCur int
}

func newListFrame(title string, items []BrowserItem) bframe {
	return bframe{kind: kindList, title: title, all: items}
}

func newDetailFrame(item BrowserItem) bframe {
	return bframe{kind: kindDetail, item: item}
}

func (f *bframe) visible() []BrowserItem {
	if f.filter == "" {
		return f.all
	}
	q := strings.ToLower(f.filter)
	var out []BrowserItem
	for _, it := range f.all {
		if strings.Contains(strings.ToLower(it.Label), q) ||
			strings.Contains(strings.ToLower(it.Meta), q) {
			out = append(out, it)
		}
	}
	return out
}

// ── model ────────────────────────────────────────────────────────────────────

type childLoadedMsg struct {
	title string
	items []BrowserItem
	err   error
}

type browser struct {
	stack   []bframe
	width   int
	height  int
	loading bool
	loadErr string
}

func (m *browser) top() *bframe { return &m.stack[len(m.stack)-1] }

func (m *browser) Init() tea.Cmd { return nil }

func (m *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case childLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err.Error()
		} else {
			m.loadErr = ""
			m.stack = append(m.stack, newListFrame(msg.title, msg.items))
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
	case kindList:
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
			m.stack = append(m.stack, newDetailFrame(v[f.cursor]))
		}
	case "o":
		if f.cursor < len(v) && v[f.cursor].WebURL != "" {
			_ = OpenURL(v[f.cursor].WebURL)
		}
	}
	return m, nil
}

func (m *browser) handleDetailKey(f *bframe, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.stack = m.stack[:len(m.stack)-1]
	case "left", "h", "shift+tab":
		if f.linkCur > 0 {
			f.linkCur--
		}
	case "right", "l", "tab":
		if f.linkCur < len(f.item.Links)-1 {
			f.linkCur++
		}
	case "enter":
		if len(f.item.Links) == 0 {
			break
		}
		link := f.item.Links[f.linkCur]
		m.loading = true
		return m, func() tea.Msg {
			title, items, err := link.Load()
			return childLoadedMsg{title: title, items: items, err: err}
		}
	case "o":
		if f.item.WebURL != "" {
			_ = OpenURL(f.item.WebURL)
		}
	}
	return m, nil
}

// ── view ─────────────────────────────────────────────────────────────────────

func (m *browser) View() string {
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
		case kindList:
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

	if f.filtering {
		sb.WriteString("  / " + brFilter.Render(f.filter) + "█\n\n")
	} else if f.filter != "" {
		sb.WriteString("  / " + brDim.Render(f.filter) + "  " + brDim.Render("(esc to clear)") + "\n\n")
	} else {
		sb.WriteString("  " + brDim.Render("press / to filter") + "\n\n")
	}

	// Calculate scroll window
	headerH := 6
	hintH := 2
	listH := m.height - headerH - hintH
	if listH < 3 {
		listH = 3
	}
	start := 0
	if f.cursor >= listH {
		start = f.cursor - listH + 1
	}
	end := start + listH
	if end > len(v) {
		end = len(v)
	}

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

	if len(v) > listH {
		sb.WriteString("\n  " + brDim.Render(fmt.Sprintf("%d–%d of %d  ↑↓ to scroll", start+1, end, len(v))) + "\n")
	}

	sep := brDim.Render(strings.Repeat("─", brMax(m.width-4, 10)))
	sb.WriteString("\n  " + sep + "\n")
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

func (m *browser) viewDetail(f *bframe) string {
	var sb strings.Builder

	label := f.item.Label
	if f.item.ID != "" && f.item.ID != f.item.Label {
		label = f.item.ID
	}
	sb.WriteString(m.renderHeader(label))
	sb.WriteString("\n")

	// Key-value fields
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

	// Child navigation links
	if len(f.item.Links) > 0 {
		sb.WriteString("\n  " + brSection.Render("Navigate to") + "\n  ")
		for i, link := range f.item.Links {
			if i > 0 {
				sb.WriteString("  ")
			}
			if i == f.linkCur {
				sb.WriteString(brLinkSel.Render(" " + link.Label + " "))
			} else {
				sb.WriteString(brLink.Render(" " + link.Label + " "))
			}
		}
		sb.WriteString("\n")
	}

	// Hint bar
	var hints []string
	if len(f.item.Links) > 0 {
		hints = append(hints, "←→ select", "enter open")
	}
	if f.item.WebURL != "" {
		hints = append(hints, "o web")
	}
	hints = append(hints, "esc back", "q quit")

	sep := brDim.Render(strings.Repeat("─", brMax(m.width-4, 10)))
	sb.WriteString("\n  " + sep + "\n")
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
	brLink     = lipgloss.NewStyle().
			Foreground(lipgloss.Color("33")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
	brLinkSel = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10"))
)

// ── helpers ──────────────────────────────────────────────────────────────────

func brTrunc(s string, maxLen int) string {
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
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
