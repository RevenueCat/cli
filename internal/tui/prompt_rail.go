package tui

import (
	"errors"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/revenuecat/cli/internal/output"
)

// Rail-native prompts: confirm / select / text-input rendered on the Flow's
// vertical spine (clack-style ◆ active → ◇ resolved), so a guided flow stays one
// cohesive experience instead of popping out separate boxes. They're methods on
// Flow. huh remains the prompt layer for ordinary (data) commands.

// ErrPromptCancelled is returned when the user aborts a rail prompt (esc/ctrl-c).
var ErrPromptCancelled = errors.New("cancelled")

type Option struct {
	Label string
	Value string
}

var (
	prRailSty   = lipgloss.NewStyle().Foreground(output.BrandRed)
	prActiveSty = lipgloss.NewStyle().Foreground(output.BrandRed).Bold(true)
	prTitleSty  = lipgloss.NewStyle().Bold(true)
	prSelSty    = lipgloss.NewStyle().Foreground(output.AccentViolet).Bold(true)
	prDimSty    = lipgloss.NewStyle().Faint(true)
	prOKSty     = lipgloss.NewStyle().Foreground(output.GreenOK).Bold(true)
	prWarnSty   = lipgloss.NewStyle().Foreground(output.WarnAmber)
)

func railSpacer() string { return prRailSty.Render("│") }
func railHead(glyph, title string) string {
	return prActiveSty.Render(glyph) + "  " + prTitleSty.Render(title)
}
func railBody(s string) string { return prRailSty.Render("│") + "  " + s }

// Confirm returns def without prompting in plain mode.
func (f *Flow) Confirm(question string, def bool) (bool, error) {
	if f.plain {
		return def, nil
	}
	m, err := runRailPrompt(f.w, confirmModel{title: question, yes: def})
	if err != nil {
		return false, err
	}
	cm := m.(confirmModel)
	if cm.cancelled {
		return false, ErrPromptCancelled
	}
	return cm.yes, nil
}

func (f *Flow) Select(title string, opts []Option, desc ...string) (string, error) {
	if f.plain {
		return "", errors.New("cannot show a picker in non-interactive mode")
	}
	if len(opts) == 0 {
		return "", errors.New("no options to choose from")
	}
	m, err := runRailPrompt(f.w, selectModel{title: title, desc: desc, opts: opts})
	if err != nil {
		return "", err
	}
	sm := m.(selectModel)
	if sm.cancelled {
		return "", ErrPromptCancelled
	}
	return sm.opts[sm.cursor].Value, nil
}

// Input's validate may be nil.
func (f *Flow) Input(title, placeholder string, validate func(string) error, desc ...string) (string, error) {
	if f.plain {
		return "", errors.New("cannot prompt for input in non-interactive mode")
	}
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	m, err := runRailPrompt(f.w, inputModel{title: title, desc: desc, ti: ti, validate: validate})
	if err != nil {
		return "", err
	}
	im := m.(inputModel)
	if im.cancelled {
		return "", ErrPromptCancelled
	}
	return strings.TrimSpace(im.ti.Value()), nil
}

// Password is like Input but masks the entry and never echoes the value back in
// the resolved receipt.
func (f *Flow) Password(title string, validate func(string) error, desc ...string) (string, error) {
	if f.plain {
		return "", errors.New("cannot prompt for input in non-interactive mode")
	}
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()
	m, err := runRailPrompt(f.w, inputModel{title: title, desc: desc, ti: ti, validate: validate, masked: true})
	if err != nil {
		return "", err
	}
	im := m.(inputModel)
	if im.cancelled {
		return "", ErrPromptCancelled
	}
	// Never trim a password — leading/trailing spaces can be significant.
	return im.ti.Value(), nil
}

func runRailPrompt(w io.Writer, m tea.Model) (tea.Model, error) {
	return tea.NewProgram(m, tea.WithOutput(w)).Run()
}

// ---- confirm ----

type confirmModel struct {
	title     string
	yes       bool
	done      bool
	cancelled bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "left", "right", "tab", "h", "l":
		m.yes = !m.yes
	case "y", "Y":
		m.yes, m.done = true, true
		return m, tea.Quit
	case "n", "N":
		m.yes, m.done = false, true
		return m, tea.Quit
	case "enter":
		m.done = true
		return m, tea.Quit
	case "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.done {
		ans := "No"
		if m.yes {
			ans = "Yes"
		}
		return railSpacer() + "\n" + railHead("◇", m.title) + "\n" + railBody(prOKSty.Render("✓")+" "+ans) + "\n"
	}
	yes, no := "Yes", "No"
	if m.yes {
		yes = prSelSty.Render("▸ Yes")
		no = prDimSty.Render("No")
	} else {
		yes = prDimSty.Render("Yes")
		no = prSelSty.Render("▸ No")
	}
	return railSpacer() + "\n" + railHead("◆", m.title) + "\n" + railBody(yes+"    "+no) + "\n"
}

// ---- select ----

type selectModel struct {
	title     string
	desc      []string
	opts      []Option
	cursor    int
	done      bool
	cancelled bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.opts)-1 {
			m.cursor++
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	case "esc", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	b.WriteString(railSpacer() + "\n")
	if m.done {
		b.WriteString(railHead("◇", m.title) + "\n")
		b.WriteString(railBody(prOKSty.Render("✓")+" "+m.opts[m.cursor].Label) + "\n")
		return b.String()
	}
	b.WriteString(railHead("◆", m.title) + "\n")
	for _, d := range m.desc {
		b.WriteString(railBody(prDimSty.Render(d)) + "\n")
	}
	for i, o := range m.opts {
		if i == m.cursor {
			b.WriteString(railBody(prSelSty.Render("▸ "+o.Label)) + "\n")
		} else {
			b.WriteString(railBody("  "+o.Label) + "\n")
		}
	}
	return b.String()
}

// ---- input ----

type inputModel struct {
	title     string
	desc      []string
	ti        textinput.Model
	validate  func(string) error
	masked    bool
	errMsg    string
	done      bool
	cancelled bool
}

func (m inputModel) Init() tea.Cmd { return textinput.Blink }

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			v := strings.TrimSpace(m.ti.Value())
			if m.validate != nil {
				if err := m.validate(v); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
			}
			m.done = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	var b strings.Builder
	b.WriteString(railSpacer() + "\n")
	if m.done {
		shown := strings.TrimSpace(m.ti.Value())
		if m.masked {
			shown = "••••••"
		}
		b.WriteString(railHead("◇", m.title) + "\n")
		b.WriteString(railBody(prOKSty.Render("✓")+" "+shown) + "\n")
		return b.String()
	}
	b.WriteString(railHead("◆", m.title) + "\n")
	for _, d := range m.desc {
		b.WriteString(railBody(prDimSty.Render(d)) + "\n")
	}
	b.WriteString(railBody(m.ti.View()) + "\n")
	if m.errMsg != "" {
		b.WriteString(railBody(lipgloss.NewStyle().Foreground(output.ErrorRed).Render("! "+m.errMsg)) + "\n")
	}
	return b.String()
}
