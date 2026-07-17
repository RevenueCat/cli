package output

import "github.com/charmbracelet/lipgloss"

// RevenueCat brand palette, taken from the dashboard's design tokens
// (revenuecat-app src/styles/theme.css + palette-primitives.css). Each color
// carries a light- and dark-background variant; lipgloss picks per terminal
// and degrades gracefully on 256-color terminals.
var (
	// BrandRed is RevenueCat red — identity moments only (titles, accents),
	// never errors, so red text still means something is wrong.
	BrandRed = lipgloss.AdaptiveColor{Light: "#D40017", Dark: "#F2545B"}
	// ErrorRed is deeper than brand red so failures read as failures.
	ErrorRed  = lipgloss.AdaptiveColor{Light: "#B70004", Dark: "#F03F3C"}
	GreenOK   = lipgloss.AdaptiveColor{Light: "#00845E", Dark: "#11D483"}
	WarnAmber = lipgloss.AdaptiveColor{Light: "#AC592D", Dark: "#E79462"}
	InfoBlue  = lipgloss.AdaptiveColor{Light: "#4F62C4", Dark: "#7D8EF2"}
)

// Shared semantic styles. Commands and TUIs should use these instead of
// hand-picking ANSI colors so the whole CLI reads as one product.
var (
	StyleTitle   = lipgloss.NewStyle().Bold(true)
	StyleAccent  = lipgloss.NewStyle().Foreground(BrandRed).Bold(true)
	StyleSuccess = lipgloss.NewStyle().Foreground(GreenOK).Bold(true)
	StyleWarn    = lipgloss.NewStyle().Foreground(WarnAmber)
	StyleError   = lipgloss.NewStyle().Foreground(ErrorRed).Bold(true)
	StyleInfo    = lipgloss.NewStyle().Foreground(InfoBlue)
	StyleDim     = lipgloss.NewStyle().Faint(true)
)
