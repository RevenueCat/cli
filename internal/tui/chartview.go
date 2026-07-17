package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/barchart"
	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/linechart"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/revenuecat/cli/internal/api"
)

const (
	minBarWidth  = 5 // minimum chars per bar before we switch to paginated mode
	pagedBarW    = 3 // bar width when paginating
	pagedBarGap  = 1 // gap when paginating
	pagedBarStep = pagedBarW + pagedBarGap
	targetTicks  = 6 // target number of y-axis tick marks
)

// toolbar option definitions
var chartTypes = []string{"Bar", "Line"}

type resolutionOpt struct{ label, id string }

var resolutionOptions = []resolutionOpt{
	{"Day", "0"}, {"Week", "1"}, {"Month", "2"}, {"Quarter", "3"}, {"Year", "4"},
}

type rangeOpt struct {
	label  string
	months int // 0 = all time
}

var rangeOptions = []rangeOpt{
	{"1M", 1}, {"3M", 3}, {"6M", 6}, {"1Y", 12}, {"2Y", 24}, {"All", 0},
}

type processedBar struct {
	label      string
	value      float64
	incomplete bool
}

type fetchResultMsg struct {
	data *api.ChartData
	err  error
}

type chartApp struct {
	// toolbar
	groupFocus    int // 0=type, 1=resolution, 2=range
	chartTypeIdx  int
	resolutionIdx int // default: 2 (Month)
	rangeIdx      int // default: 2 (6M)

	// data
	bars   []processedBar
	maxVal float64
	unit   string
	title  string

	// fetching
	fetchFn  func(resID string, startUnix int64) (*api.ChartData, error)
	loading  bool
	fetchErr error

	// bar chart scroll offset
	barOffset int

	// dimensions
	termW, termH int

	// styles
	noColor         bool
	completeStyle   lipgloss.Style
	incompleteStyle lipgloss.Style
}

func (m *chartApp) Init() tea.Cmd {
	return m.doFetch()
}

func (m *chartApp) doFetch() tea.Cmd {
	resID := resolutionOptions[m.resolutionIdx].id
	months := rangeOptions[m.rangeIdx].months
	var startUnix int64
	if months > 0 {
		startUnix = time.Now().UTC().AddDate(0, -months, 0).Unix()
	}
	fn := m.fetchFn
	return func() tea.Msg {
		data, err := fn(resID, startUnix)
		return fetchResultMsg{data: data, err: err}
	}
}

func (m *chartApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW = msg.Width
		m.termH = msg.Height

	case fetchResultMsg:
		m.loading = false
		m.fetchErr = msg.err
		if msg.err == nil && msg.data != nil {
			m.bars, m.maxVal, m.unit = processBars(msg.data, m.completeStyle, m.incompleteStyle)
			if m.title == "" {
				m.title = msg.data.DisplayName
			}
		}
		// Reset scroll to end
		ps := m.barPageSize()
		m.barOffset = len(m.bars) - ps
		if m.barOffset < 0 {
			m.barOffset = 0
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit

		case "tab":
			m.groupFocus = (m.groupFocus + 1) % 3

		case "shift+tab":
			m.groupFocus = (m.groupFocus + 2) % 3

		case "left", "h":
			switch m.groupFocus {
			case 0:
				if m.chartTypeIdx > 0 {
					m.chartTypeIdx--
				}
			case 1:
				if m.resolutionIdx > 0 {
					m.resolutionIdx--
					m.loading = true
					return m, m.doFetch()
				}
			case 2:
				if m.rangeIdx > 0 {
					m.rangeIdx--
					m.loading = true
					return m, m.doFetch()
				}
			}

		case "right", "l":
			switch m.groupFocus {
			case 0:
				if m.chartTypeIdx < len(chartTypes)-1 {
					m.chartTypeIdx++
				}
			case 1:
				if m.resolutionIdx < len(resolutionOptions)-1 {
					m.resolutionIdx++
					m.loading = true
					return m, m.doFetch()
				}
			case 2:
				if m.rangeIdx < len(rangeOptions)-1 {
					m.rangeIdx++
					m.loading = true
					return m, m.doFetch()
				}
			}

		case "[":
			m.barOffset--
			m.clampBarOffset()
		case "]":
			m.barOffset++
			m.clampBarOffset()
		case "{":
			m.barOffset -= m.barPageSize()
			m.clampBarOffset()
		case "}":
			m.barOffset += m.barPageSize()
			m.clampBarOffset()
		}
	}
	return m, nil
}

func (m *chartApp) clampBarOffset() {
	ps := m.barPageSize()
	max := len(m.bars) - ps
	if max < 0 {
		max = 0
	}
	if m.barOffset > max {
		m.barOffset = max
	}
	if m.barOffset < 0 {
		m.barOffset = 0
	}
}

func (m *chartApp) chartH() int {
	h := m.termH - 7
	if h < 8 {
		return 8
	}
	return h
}

func (m *chartApp) chartW() int {
	return m.termW - 2 - m.labelWidth()
}

func (m *chartApp) labelWidth() int {
	return maxTickLabelWidth(m.maxVal, m.unit)
}

func (m *chartApp) barPageSize() int {
	const minBW = 5
	cw := m.chartW()
	if len(m.bars)*(minBW+1) <= cw {
		return len(m.bars)
	}
	n := cw / 4
	if n < 1 {
		return 1
	}
	return n
}

func (m *chartApp) buildBarView() string {
	if len(m.bars) == 0 {
		return "  no data"
	}

	pageSize := m.barPageSize()
	end := m.barOffset + pageSize
	if end > len(m.bars) {
		end = len(m.bars)
	}
	visible := m.bars[m.barOffset:end]

	barData := make([]barchart.BarData, len(visible))
	for i, b := range visible {
		sty := m.completeStyle
		if b.incomplete {
			sty = m.incompleteStyle
		}
		barData[i] = barchart.BarData{
			Label: b.label,
			Values: []barchart.BarValue{
				{Name: "", Value: b.value, Style: sty},
			},
		}
	}

	cw := m.chartW()
	ch := m.chartH()

	var opts []barchart.Option
	opts = append(opts, barchart.WithMaxValue(m.maxVal), barchart.WithDataSet(barData))
	paginated := len(m.bars)*(minBarWidth+1) > cw
	if paginated {
		opts = append(opts,
			barchart.WithNoAutoBarWidth(),
			barchart.WithBarWidth(pagedBarW),
			barchart.WithBarGap(pagedBarGap),
		)
	} else {
		opts = append(opts, barchart.WithBarGap(1))
	}

	bc := barchart.New(cw, ch, opts...)
	bc.Draw()

	yLabelStyle := lipgloss.NewStyle().Faint(true)
	chartLines := strings.Split(bc.View(), "\n")
	barAreaH := len(chartLines) - 2
	if barAreaH < 1 {
		barAreaH = 1
	}

	lw := m.labelWidth()
	ticks := niceTickRows(m.maxVal, barAreaH, targetTicks)
	tickLabels := make(map[int]string, len(ticks))
	for _, t := range ticks {
		tickLabels[t.row] = padLeftStr(fmtChartVal(t.val, m.unit), lw)
	}
	indent := strings.Repeat(" ", lw+1)

	var sb strings.Builder
	for i, line := range chartLines {
		if i < barAreaH {
			if label, ok := tickLabels[i]; ok {
				sb.WriteString(yLabelStyle.Render(label) + " " + line + "\n")
			} else {
				sb.WriteString(indent + line + "\n")
			}
		} else {
			sb.WriteString(indent + line + "\n")
		}
	}
	return sb.String()
}

func (m *chartApp) buildLineView() string {
	bars := m.bars
	if len(bars) == 0 {
		return "  no data"
	}

	w := m.chartW() + m.labelWidth()
	h := m.chartH()
	maxX := float64(len(bars) - 1)
	if maxX < 1 {
		maxX = 1
	}

	labels := make([]string, len(bars))
	for i, b := range bars {
		labels[i] = b.label
	}
	xStep := len(bars) / 8
	if xStep < 1 {
		xStep = 1
	}
	yStep := h / 6
	if yStep < 1 {
		yStep = 1
	}

	unit := m.unit
	maxVal := m.maxVal

	lc := linechart.New(w, h, 0, maxX, 0, maxVal,
		linechart.WithXLabelFormatter(func(i int, _ float64) string {
			if i >= 0 && i < len(labels) {
				return labels[i]
			}
			return ""
		}),
		linechart.WithYLabelFormatter(func(_ int, v float64) string {
			return fmtChartVal(v, unit)
		}),
		linechart.WithXYSteps(xStep, yStep),
	)
	lc.DrawXYAxisAndLabel()

	for i := 1; i < len(bars); i++ {
		p1 := canvas.Float64Point{X: float64(i - 1), Y: bars[i-1].value}
		p2 := canvas.Float64Point{X: float64(i), Y: bars[i].value}
		sty := m.completeStyle
		if bars[i].incomplete {
			sty = m.incompleteStyle
		}
		lc.DrawBrailleLineWithStyle(p1, p2, sty)
	}

	return lc.View()
}

func (m *chartApp) renderToolbar() string {
	faintStyle := lipgloss.NewStyle().Faint(true)
	selectedFocused := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	selectedUnfocused := lipgloss.NewStyle().Bold(true)

	renderGroup := func(groupIdx int, label string, options []string, selectedIdx int) string {
		var sb strings.Builder
		prefix := "  "
		if m.groupFocus == groupIdx {
			prefix = "▸ "
		}
		sb.WriteString(prefix)
		sb.WriteString(faintStyle.Render(label+":") + " ")
		for i, opt := range options {
			if i > 0 {
				sb.WriteString(" ")
			}
			if i == selectedIdx {
				if m.groupFocus == groupIdx {
					sb.WriteString(selectedFocused.Render(opt))
				} else {
					sb.WriteString(selectedUnfocused.Render(opt))
				}
			} else {
				sb.WriteString(opt)
			}
		}
		return sb.String()
	}

	// Build resolution option labels
	resLabels := make([]string, len(resolutionOptions))
	for i, r := range resolutionOptions {
		resLabels[i] = r.label
	}

	// Build range option labels
	rangeLabels := make([]string, len(rangeOptions))
	for i, r := range rangeOptions {
		rangeLabels[i] = r.label
	}

	sep := faintStyle.Render("  │  ")
	return renderGroup(0, "Type", chartTypes, m.chartTypeIdx) +
		sep +
		renderGroup(1, "Resolution", resLabels, m.resolutionIdx) +
		sep +
		renderGroup(2, "Range", rangeLabels, m.rangeIdx)
}

func (m *chartApp) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	faintStyle := lipgloss.NewStyle().Faint(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(m.title) + "\n")

	// subtitle: resolution · unit
	subtitle := buildChartSubtitleFromParts(resolutionOptions[m.resolutionIdx].label, m.unit)
	if m.loading {
		subtitle += "  · fetching…"
	}
	if subtitle != "" {
		sb.WriteString(faintStyle.Render(subtitle) + "\n")
	}
	sb.WriteString("\n")

	if m.fetchErr != nil && !m.loading {
		sb.WriteString(fmt.Sprintf("  error: %v\n", m.fetchErr))
	} else {
		if m.chartTypeIdx == 0 {
			sb.WriteString(m.buildBarView())
		} else {
			sb.WriteString(m.buildLineView() + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(m.renderToolbar() + "\n")
	sb.WriteString(faintStyle.Render("[/] scroll bar  tab switch group  q quit"))
	return sb.String()
}

// processBars converts ChartData values into processedBar entries, filtering
// future dates. It also returns the maxVal and unit.
func processBars(data *api.ChartData, completeStyle, incompleteStyle lipgloss.Style) ([]processedBar, float64, string) {
	now := time.Now().UTC()
	var maxVal float64
	var bars []processedBar
	for _, v := range data.Values {
		t := time.Unix(v.Cohort, 0).UTC()
		if t.After(now) {
			continue
		}
		if v.Value > maxVal {
			maxVal = v.Value
		}
		bars = append(bars, processedBar{
			label:      chartDateLabel(t, data.Resolution),
			value:      v.Value,
			incomplete: v.Incomplete,
		})
	}
	if maxVal == 0 {
		maxVal = 1
	}
	unit := data.YAxis
	return bars, maxVal, unit
}

// resolutionIDFromString maps the server resolution string to our toolbar index.
func resolutionIDFromString(res string) int {
	switch strings.ToLower(res) {
	case "day":
		return 0
	case "week":
		return 1
	case "month":
		return 2
	case "quarter":
		return 3
	case "year":
		return 4
	}
	return 2 // default: month
}

// buildChartSubtitleFromParts builds a subtitle from the resolution label and unit.
func buildChartSubtitleFromParts(resLabel, unit string) string {
	if unit == "" {
		return resLabel
	}
	return unit + "  ·  " + resLabel
}

// RunChartView runs an interactive scrollable chart viewer. Returns an error if
// stdout or stdin is not a TTY — callers should fall back to --json.
func RunChartView(
	data *api.ChartData,
	fetchFn func(resID string, startUnix int64) (*api.ChartData, error),
	noColor bool,
) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("chart view requires a terminal — pass --json for machine-readable output")
	}

	completeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	incompleteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if noColor {
		completeStyle = lipgloss.NewStyle()
		incompleteStyle = lipgloss.NewStyle().Faint(true)
	}

	bars, maxVal, unit := processBars(data, completeStyle, incompleteStyle)

	resIdx := resolutionIDFromString(data.Resolution)

	app := &chartApp{
		groupFocus:      0,
		chartTypeIdx:    0,
		resolutionIdx:   resIdx,
		rangeIdx:        2, // 6M default
		bars:            bars,
		maxVal:          maxVal,
		unit:            unit,
		title:           data.DisplayName,
		fetchFn:         fetchFn,
		loading:         false,
		noColor:         noColor,
		completeStyle:   completeStyle,
		incompleteStyle: incompleteStyle,
	}

	// Set terminal size
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 100, 30
	}
	app.termW = w
	app.termH = h

	// Start anchored at the most recent data
	ps := app.barPageSize()
	app.barOffset = len(bars) - ps
	if app.barOffset < 0 {
		app.barOffset = 0
	}

	_, err = tea.NewProgram(app, tea.WithAltScreen()).Run()
	return err
}

// tick holds a y-axis tick value and the row it maps to.
type tick struct {
	val float64
	row int
}

// niceTickRows computes ~numTicks evenly spaced y-axis tick marks with "nice"
// round values. Row 0 is the top (maxVal); row barAreaH is the axis line ($0).
func niceTickRows(maxVal float64, barAreaH, numTicks int) []tick {
	step := niceStep(maxVal, numTicks)
	var ticks []tick
	for v := 0.0; v <= maxVal+step*0.01; v += step {
		if v > maxVal {
			v = maxVal
		}
		// row 0 = maxVal, row barAreaH = 0
		row := int(math.Round(float64(barAreaH) * (1 - v/maxVal)))
		ticks = append(ticks, tick{val: v, row: row})
	}
	return ticks
}

// niceStep picks a human-friendly step size for ~numTicks ticks over [0, maxVal].
func niceStep(maxVal float64, numTicks int) float64 {
	if maxVal <= 0 {
		return 1
	}
	rough := maxVal / float64(numTicks)
	mag := math.Pow(10, math.Floor(math.Log10(rough)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*mag >= rough {
			return m * mag
		}
	}
	return mag * 10
}

// maxTickLabelWidth returns the widest label we'll generate for this axis.
func maxTickLabelWidth(maxVal float64, unit string) int {
	step := niceStep(maxVal, targetTicks)
	w := 0
	for v := 0.0; v <= maxVal+step*0.01; v += step {
		if l := len(fmtChartVal(v, unit)); l > w {
			w = l
		}
	}
	if l := len(fmtChartVal(maxVal, unit)); l > w {
		w = l
	}
	return w
}

func chartDateLabel(t time.Time, resolution string) string {
	switch resolution {
	case "month":
		return t.Format("Jan 06")
	default:
		return t.Format("1/2")
	}
}

func fmtChartVal(val float64, unit string) string {
	s := fmtCompact(val)
	switch unit {
	case "$":
		return "$" + s
	case "%":
		return s + "%"
	default:
		return s
	}
}

func fmtCompact(val float64) string {
	if val == 0 {
		return "0"
	}
	abs := math.Abs(val)
	switch {
	case abs >= 1_000_000:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1fM", val/1_000_000), "0"), ".")
	case abs >= 1_000:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1fK", val/1_000), "0"), ".")
	default:
		return fmt.Sprintf("%.0f", val)
	}
}

func padLeftStr(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
