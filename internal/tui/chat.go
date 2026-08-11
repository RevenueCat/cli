package tui

import (
	"context"
	"regexp"
	"strings"

	"github.com/revenuecat/cli/internal/output"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Chat is a full-screen streaming chat window (transcript + input) for agent
// conversations. The caller supplies Send, which runs one turn and reports
// progress through the ChatEmitter; the window handles rendering, scrolling,
// and tool-approval prompts.
type Chat struct {
	Title    string
	Subtitle string
	// Placeholder shown in the empty input.
	Placeholder string
	// Send runs one conversation turn. It is called on its own goroutine and
	// must stop when ctx is cancelled. It must call emit.Done exactly once.
	Send func(ctx context.Context, message string, emit *ChatEmitter)
	// Transcript preloads history (e.g. when resuming a conversation).
	Transcript []ChatEntry
	// Initial, when set, is sent as the first turn as soon as the window opens.
	Initial string
	// RelativeLinkBase, when set, prefixes relative markdown link targets
	// ("[x](/path)") in assistant messages so they are clickable in a
	// terminal (e.g. "https://app.revenuecat.com").
	RelativeLinkBase string
}

type ChatRole int

const (
	ChatUser ChatRole = iota
	ChatAssistant
	ChatTool
	ChatNotice
)

type ChatEntry struct {
	Role ChatRole
	Text string
}

// ChatEmitter delivers turn progress into the running window. Methods are
// safe to call from the Send goroutine.
type ChatEmitter struct {
	program *tea.Program
	ctx     context.Context
}

func (e *ChatEmitter) Delta(text string)    { e.program.Send(chatDeltaMsg(text)) }
func (e *ChatEmitter) ToolCall(name string) { e.program.Send(chatToolMsg(name)) }

// Done ends the turn; err is shown as a notice rather than exiting the window.
func (e *ChatEmitter) Done(err error) { e.program.Send(chatDoneMsg{err}) }

// Approve blocks until the user decides (or the window closes, which counts
// as a rejection).
func (e *ChatEmitter) Approve(prompt string, destructive bool) bool {
	response := make(chan bool, 1)
	e.program.Send(chatApprovalMsg{prompt: prompt, destructive: destructive, response: response})
	select {
	case approved := <-response:
		return approved
	case <-e.ctx.Done():
		return false
	}
}

// RunChat opens the window and blocks until the user exits.
func (c Chat) RunChat() error {
	model := newChatModel(c)
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.program = program
	_, err := program.Run()
	model.cancelTurn()
	return err
}

type chatDeltaMsg string
type chatToolMsg string
type chatDoneMsg struct{ err error }
type chatApprovalMsg struct {
	prompt      string
	destructive bool
	response    chan bool
}

type chatModel struct {
	cfg     Chat
	program *tea.Program

	viewport viewport.Model
	input    textarea.Model
	spin     spinner.Model
	markdown *glamour.TermRenderer

	entries   []ChatEntry
	streaming bool
	pending   string // assistant text accumulating during a stream
	activity  string // current tool name, "" while text is flowing
	approval  *chatApprovalMsg
	width     int
	height    int
	ready     bool

	cancel context.CancelFunc
}

var (
	chatHeaderStyle   = lipgloss.NewStyle().Foreground(output.BrandRed).Bold(true)
	chatDimStyle      = lipgloss.NewStyle().Faint(true)
	chatUserStyle     = lipgloss.NewStyle().Foreground(output.BrandRed).Bold(true)
	chatToolStyle     = lipgloss.NewStyle().Faint(true)
	chatNoticeStyle   = lipgloss.NewStyle().Foreground(output.WarnAmber)
	chatApproveStyle  = lipgloss.NewStyle().Foreground(output.GreenOK).Bold(true)
	chatDestructStyle = lipgloss.NewStyle().Foreground(output.ErrorRed).Bold(true)
	chatInputBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(output.NeutralGray).
				Padding(0, 1)
	chatInputActive = output.AccentViolet
)

func newChatModel(cfg Chat) *chatModel {
	input := textarea.New()
	input.Placeholder = cfg.Placeholder
	input.CharLimit = 0
	input.SetHeight(1)
	input.ShowLineNumbers = false
	input.Prompt = ""
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	return &chatModel{
		cfg:     cfg,
		input:   input,
		spin:    spin,
		entries: append([]ChatEntry(nil), cfg.Transcript...),
	}
}

func (m *chatModel) cancelTurn() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *chatModel) Init() tea.Cmd {
	if m.cfg.Initial != "" {
		return tea.Batch(textarea.Blink, m.startTurn(m.cfg.Initial))
	}
	return textarea.Blink
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh(true)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case chatDeltaMsg:
		m.pending += string(msg)
		m.activity = ""
		m.refresh(true)
		return m, nil

	case chatToolMsg:
		m.flushPending()
		m.activity = string(msg)
		m.entries = append(m.entries, ChatEntry{Role: ChatTool, Text: string(msg)})
		m.refresh(true)
		return m, nil

	case chatApprovalMsg:
		m.flushPending()
		m.approval = &msg
		m.refresh(true)
		return m, nil

	case chatDoneMsg:
		m.flushPending()
		m.activity = ""
		if msg.err != nil {
			m.entries = append(m.entries, ChatEntry{Role: ChatNotice, Text: msg.err.Error()})
		}
		m.streaming = false
		m.cancel = nil
		m.input.Focus()
		m.refresh(true)
		return m, textarea.Blink

	case spinner.TickMsg:
		if !m.streaming {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		m.refresh(false)
		return m, cmd
	}

	return m.updateChildren(msg)
}

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.approval != nil {
		switch msg.String() {
		case "y", "Y":
			m.resolveApproval(true)
		case "n", "N", "esc":
			m.resolveApproval(false)
		case "ctrl+c":
			m.resolveApproval(false)
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.cancelTurn()
		return m, tea.Quit
	case "esc":
		if m.streaming {
			m.cancelTurn()
			return m, nil
		}
		return m, tea.Quit
	case "enter":
		if m.streaming {
			return m, nil
		}
		message := strings.TrimSpace(m.input.Value())
		if message == "" {
			return m, nil
		}
		m.input.Reset()
		return m, m.startTurn(message)
	case "ctrl+j":
		m.input.InsertString("\n")
		return m, nil
	case "pgup":
		m.viewport.HalfPageUp()
		return m, nil
	case "pgdown":
		m.viewport.HalfPageDown()
		return m, nil
	}
	return m.updateChildren(msg)
}

func (m *chatModel) updateChildren(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	if !m.streaming && m.approval == nil {
		before := m.input.Height()
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		if m.sizeInput(); m.input.Height() != before {
			m.layout()
			m.refresh(false)
		}
	}
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *chatModel) startTurn(message string) tea.Cmd {
	m.entries = append(m.entries, ChatEntry{Role: ChatUser, Text: message})
	m.streaming = true
	m.pending = ""
	m.input.Blur()
	m.refresh(true)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	emit := &ChatEmitter{program: m.program, ctx: ctx}
	go m.cfg.Send(ctx, message, emit)
	return m.spin.Tick
}

func (m *chatModel) resolveApproval(approved bool) {
	if m.approval == nil {
		return
	}
	m.approval.response <- approved
	verdict := "rejected"
	style := chatDestructStyle
	if approved {
		verdict = "approved"
		style = chatApproveStyle
	}
	m.entries = append(m.entries, ChatEntry{
		Role: ChatNotice,
		Text: style.Render(verdict) + " · " + m.approval.prompt,
	})
	m.approval = nil
	m.refresh(true)
}

func (m *chatModel) flushPending() {
	if strings.TrimSpace(m.pending) != "" {
		m.entries = append(m.entries, ChatEntry{Role: ChatAssistant, Text: m.pending})
	}
	m.pending = ""
}

func (m *chatModel) layout() {
	m.sizeInput()
	inputHeight := m.input.Height() + 2 + 1 // border + footer
	headerHeight := 2
	viewportHeight := max(m.height-inputHeight-headerHeight-1, 1)
	if !m.ready {
		m.viewport = viewport.New(m.width, viewportHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = viewportHeight
	}
	m.input.SetWidth(max(m.width-6, 10)) // border + padding
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(max(m.width-4, 20)),
		glamour.WithEmoji(),
	)
	if err == nil {
		m.markdown = renderer
	}
}

func (m *chatModel) refresh(follow bool) {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.renderTranscript())
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *chatModel) renderTranscript() string {
	var b strings.Builder
	for _, entry := range m.entries {
		b.WriteString(m.renderEntry(entry))
	}
	if m.streaming {
		if m.pending != "" {
			// Render in-flight text through the same markdown pipeline as
			// finished messages so formatting appears as it streams instead of
			// snapping in at the end of the turn.
			b.WriteString(m.renderEntry(ChatEntry{Role: ChatAssistant, Text: m.pending}))
		}
		// Always show a live activity line so long tool executions and quiet
		// stretches of the stream never look frozen.
		label := "thinking…"
		if m.activity != "" {
			label = "running " + m.activity + "…"
		}
		b.WriteString("\n  " + m.spin.View() + " " + chatDimStyle.Render(label) + "\n")
	}
	return b.String()
}

func (m *chatModel) renderEntry(entry ChatEntry) string {
	switch entry.Role {
	case ChatUser:
		return "\n" + chatUserStyle.Render("❯ ") + entry.Text + "\n"
	case ChatTool:
		return chatToolStyle.Render("  ⚙ "+entry.Text) + "\n"
	case ChatNotice:
		return chatNoticeStyle.Render("  "+entry.Text) + "\n"
	default: // assistant
		text := entry.Text
		if m.cfg.RelativeLinkBase != "" {
			text = absolutizeLinks(text, m.cfg.RelativeLinkBase)
		}
		if m.markdown != nil {
			if rendered, err := m.markdown.Render(text); err == nil {
				return rendered
			}
		}
		return text + "\n"
	}
}

func (m *chatModel) View() string {
	if !m.ready {
		return "loading…"
	}
	header := chatHeaderStyle.Render(m.cfg.Title)
	if m.cfg.Subtitle != "" {
		header += "  " + chatDimStyle.Render(m.cfg.Subtitle)
	}

	footer := chatDimStyle.Render("enter send · ctrl+j newline · pgup/pgdn scroll · esc quit")
	switch {
	case m.approval != nil:
		label := "Allow: " + m.approval.prompt + "  "
		hint := chatApproveStyle.Render("[y] approve") + "  " + chatDestructStyle.Render("[n] reject")
		if m.approval.destructive {
			label = chatDestructStyle.Render("⚠ destructive · ") + label
		}
		footer = label + hint
	case m.streaming:
		footer = m.spin.View() + chatDimStyle.Render(" thinking… · esc to stop")
	}

	inputBox := chatInputBoxStyle
	if !m.streaming && m.approval == nil {
		inputBox = inputBox.BorderForeground(chatInputActive)
	}
	return header + "\n" +
		chatDimStyle.Render(strings.Repeat("─", max(m.width, 1))) + "\n" +
		m.viewport.View() + "\n" +
		inputBox.Width(max(m.width-2, 12)).Render(m.input.View()) + "\n" +
		footer
}

// barePathPattern matches dashboard-looking absolute paths in prose: after
// whitespace, start of text, an opening paren, or a backtick. The character
// class excludes closing punctuation so trailing ")." stays out of the URL.
var barePathPattern = regexp.MustCompile(
	"(^|[\\s(`])(/(?:projects|apps|customers|settings|overview|charts|activity|integrations|paywalls|offerings|products|experiments)[A-Za-z0-9_\\-/?=&#%]*)",
)

// absolutizeLinks rewrites relative markdown link targets and bare dashboard
// paths to absolute URLs so terminals can open them.
func absolutizeLinks(text, base string) string {
	text = strings.ReplaceAll(text, "](/", "]("+base+"/")
	return barePathPattern.ReplaceAllString(text, "${1}"+base+"${2}")
}

// sizeInput grows the textarea with its content, up to 5 lines.
func (m *chatModel) sizeInput() {
	desired := min(max(m.input.LineCount(), 1), 5)
	if m.input.Height() != desired {
		m.input.SetHeight(desired)
	}
}
