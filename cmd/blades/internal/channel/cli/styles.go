package cli

import "charm.land/lipgloss/v2"

// appStyles holds all lipgloss styles for the TUI, built once from the
// detected dark/light background so that no AdaptiveColor queries happen
// during rendering.
type appStyles struct {
	header        lipgloss.Style
	dim           lipgloss.Style
	hint          lipgloss.Style
	err           lipgloss.Style
	userLabel     lipgloss.Style
	assistLabel   lipgloss.Style
	toolLabel     lipgloss.Style
	toolBox       lipgloss.Style
	toolBoxActive lipgloss.Style
	inputBox      lipgloss.Style
	statusBar     lipgloss.Style
}

func newStyles(isDark bool) appStyles {
	ld := lipgloss.LightDark(isDark)

	primary := ld(lipgloss.Color("#111827"), lipgloss.Color("#F9FAFB"))
	success := ld(lipgloss.Color("#065F46"), lipgloss.Color("#D1FAE5"))
	errClr := ld(lipgloss.Color("#DC2626"), lipgloss.Color("#F87171"))
	userClr := ld(lipgloss.Color("#1D4ED8"), lipgloss.Color("#93C5FD"))
	dim := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	panel := ld(lipgloss.Color("#CBD5E1"), lipgloss.Color("#475569"))
	panelActive := ld(lipgloss.Color("#1D4ED8"), lipgloss.Color("#93C5FD"))
	toolClr := ld(lipgloss.Color("#92400E"), lipgloss.Color("#FCD34D"))

	return appStyles{
		header:      lipgloss.NewStyle().Bold(true).Foreground(primary),
		dim:         lipgloss.NewStyle().Foreground(dim),
		hint:        lipgloss.NewStyle().Foreground(dim),
		err:         lipgloss.NewStyle().Bold(true).Foreground(errClr),
		userLabel:   lipgloss.NewStyle().Bold(true).Foreground(userClr),
		assistLabel: lipgloss.NewStyle().Bold(true).Foreground(success),
		toolLabel:   lipgloss.NewStyle().Bold(true).Foreground(toolClr),
		toolBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(panel).
			Padding(0, 1),
		toolBoxActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(panelActive).
			Padding(0, 1),
		inputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(panelActive).
			Padding(0, 1),
		statusBar: lipgloss.NewStyle().Foreground(dim),
	}
}
