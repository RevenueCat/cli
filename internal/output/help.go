package output

// Help* style cobra help sections, gated by the color arg. They live here so
// the cli package styles help without importing lipgloss directly.

func HelpHeader(color bool, s string) string {
	if !color {
		return s
	}
	return StyleTitle.Render(s)
}

func HelpCommand(color bool, s string) string {
	if !color {
		return s
	}
	return StyleCommand.Render(s)
}

func HelpDim(color bool, s string) string {
	if !color {
		return s
	}
	return StyleDim.Render(s)
}
