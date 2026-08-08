package output

// Help* style the sections of a cobra help/usage page. The color argument
// reflects the --no-color flag; lipgloss additionally drops styling on
// non-TTY output and when NO_COLOR is set, so piped help stays plain. They
// live here so the cli package styles help through the same palette as
// everything else without importing lipgloss directly.

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
