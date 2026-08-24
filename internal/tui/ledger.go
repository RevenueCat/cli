package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/revenuecat/cli/internal/output"
)

// A Ledger renders a fixed list of steps that fill in as they run: a spinner on
// the active step, a green check when it finishes, a red cross on failure, and a
// dim marker for steps not yet reached. On a TTY it repaints in place
// (bubbletea inline — no alt-screen, scrollback preserved). When not on a TTY
// (or plain is set), it prints append-only lines instead, so CI logs stay clean.
//
// Usage:
//
//	l := tui.NewLedger(os.Stderr, plain, "Enable APIs", "Create service account")
//	l.Start()
//	l.Running(0); /* work */; l.Done(0, "")
//	l.Running(1); /* work */; l.Fail(1, "quota exceeded")
//	l.Stop()
type Ledger struct {
	w     io.Writer
	plain bool
	steps []ledgerStep
	prog  *tea.Program
	done  chan struct{}
}

type ledgerStatus int

const (
	ledgerPending ledgerStatus = iota
	ledgerRunning
	ledgerDone
	ledgerFailed
)

type ledgerStep struct {
	label  string
	note   string
	status ledgerStatus
}

var (
	ledgerCheck   = lipgloss.NewStyle().Foreground(output.GreenOK).Bold(true)
	ledgerCross   = lipgloss.NewStyle().Foreground(output.ErrorRed).Bold(true)
	ledgerPendSty = lipgloss.NewStyle().Foreground(output.NeutralGray)
	ledgerNoteSty = lipgloss.NewStyle().Faint(true)
)

// NewLedger builds a ledger for the given ordered step labels.
func NewLedger(w io.Writer, plain bool, labels ...string) *Ledger {
	steps := make([]ledgerStep, len(labels))
	for i, l := range labels {
		steps[i] = ledgerStep{label: l}
	}
	return &Ledger{w: w, plain: plain, steps: steps}
}

// Start begins rendering. On a TTY it launches the live region; in plain mode it
// is a no-op (lines are printed as steps resolve).
func (l *Ledger) Start() {
	if l.plain {
		return
	}
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	// This is a display-only spinner running while the caller does the real
	// work on its own goroutine. WithInput(nil) keeps it from grabbing stdin
	// (raw mode, eaten keystrokes), and WithoutSignalHandler leaves Ctrl+C to
	// the process — otherwise it would quit only the spinner while setup keeps
	// mutating cloud resources, so a cancel could look successful.
	l.prog = tea.NewProgram(ledgerModel{steps: l.steps, sp: sp},
		tea.WithOutput(l.w), tea.WithInput(nil), tea.WithoutSignalHandler())
	l.done = make(chan struct{})
	go func() {
		_, _ = l.prog.Run()
		close(l.done)
	}()
}

// Running marks step i as the active one (spinner).
func (l *Ledger) Running(i int) { l.set(i, ledgerRunning, "") }

// Done marks step i complete, with an optional trailing note.
func (l *Ledger) Done(i int, note string) { l.set(i, ledgerDone, note) }

// Fail marks step i failed, with a reason.
func (l *Ledger) Fail(i int, note string) { l.set(i, ledgerFailed, note) }

// Stop finishes the live region, leaving the final frame in scrollback.
func (l *Ledger) Stop() {
	if l.plain || l.prog == nil {
		return
	}
	l.prog.Send(ledgerQuitMsg{})
	<-l.done
}

func (l *Ledger) set(i int, status ledgerStatus, note string) {
	if i < 0 || i >= len(l.steps) {
		return
	}
	if l.plain {
		// Plain mode owns l.steps outright (no bubbletea goroutine).
		l.steps[i].status = status
		l.steps[i].note = note
		l.printPlain(l.steps[i])
		return
	}
	// TTY: the bubbletea goroutine owns the model's copy of the steps — mutate it
	// only through the message, never l.steps here, or the two goroutines race.
	if l.prog != nil {
		l.prog.Send(ledgerStatusMsg{index: i, status: status, note: note})
	}
}

// printPlain emits one append-only line per terminal state (running is silent to
// keep non-TTY logs to one line per step).
func (l *Ledger) printPlain(s ledgerStep) {
	switch s.status {
	case ledgerDone:
		if s.note != "" {
			fmt.Fprintf(l.w, "✓ %s  %s\n", s.label, s.note)
		} else {
			fmt.Fprintf(l.w, "✓ %s\n", s.label)
		}
	case ledgerFailed:
		fmt.Fprintf(l.w, "✗ %s  %s\n", s.label, s.note)
	}
}

// ledgerModel is the bubbletea model driving the live (TTY) render.
type ledgerModel struct {
	steps []ledgerStep
	sp    spinner.Model
}

type ledgerStatusMsg struct {
	index  int
	status ledgerStatus
	note   string
}

type ledgerQuitMsg struct{}

func (m ledgerModel) Init() tea.Cmd { return m.sp.Tick }

func (m ledgerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ledgerStatusMsg:
		if msg.index >= 0 && msg.index < len(m.steps) {
			m.steps[msg.index].status = msg.status
			m.steps[msg.index].note = msg.note
		}
		return m, nil
	case ledgerQuitMsg:
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m ledgerModel) View() string {
	return renderLedger(m.steps, m.sp.View())
}

// renderLedger draws the step list; spinnerFrame is the glyph for the running
// step. Pulled out so it can be unit-tested without a running program.
func renderLedger(steps []ledgerStep, spinnerFrame string) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.status {
		case ledgerDone:
			b.WriteString("  " + ledgerCheck.Render("✓") + " " + s.label)
		case ledgerFailed:
			b.WriteString("  " + ledgerCross.Render("✗") + " " + s.label)
		case ledgerRunning:
			b.WriteString("  " + spinnerFrame + s.label)
		default:
			b.WriteString("  " + ledgerPendSty.Render("○ "+s.label))
		}
		if s.note != "" {
			b.WriteString(ledgerNoteSty.Render("  " + s.note))
		}
		b.WriteString("\n")
	}
	return b.String()
}
