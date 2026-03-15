package cli

import "charm.land/lipgloss/v2"

// appStyles holds all lipgloss styles for the TUI, built once from the
// detected dark/light background so that no AdaptiveColor queries happen
// during rendering.
type appStyles struct {
	banner       lipgloss.Style
	dim          lipgloss.Style
	hint         lipgloss.Style
	err          lipgloss.Style
	userLabel    lipgloss.Style
	assistLabel  lipgloss.Style
	toolHeader   lipgloss.Style
	toolBox      lipgloss.Style
	toolBoxActive lipgloss.Style
	inputBorder  lipgloss.Style
	statusBar    lipgloss.Style
}

func newStyles(isDark bool) appStyles {
	ld := lipgloss.LightDark(isDark)

	primary := ld(lipgloss.Color("#7C3AED"), lipgloss.Color("#A78BFA"))
	success := ld(lipgloss.Color("#059669"), lipgloss.Color("#34D399"))
	warning := ld(lipgloss.Color("#D97706"), lipgloss.Color("#FCD34D"))
	errClr  := ld(lipgloss.Color("#DC2626"), lipgloss.Color("#F87171"))
	userClr := ld(lipgloss.Color("#2563EB"), lipgloss.Color("#60A5FA"))
	dim     := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))

	return appStyles{
		banner:      lipgloss.NewStyle().Bold(true).Foreground(primary),
		dim:         lipgloss.NewStyle().Foreground(dim),
		hint:        lipgloss.NewStyle().Foreground(dim).Italic(true),
		err:         lipgloss.NewStyle().Bold(true).Foreground(errClr),
		userLabel:   lipgloss.NewStyle().Bold(true).Foreground(userClr),
		assistLabel: lipgloss.NewStyle().Bold(true).Foreground(success),
		toolHeader:  lipgloss.NewStyle().Bold(true).Foreground(warning),

		toolBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dim).
			PaddingLeft(1).PaddingRight(1),

		toolBoxActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(warning).
			PaddingLeft(1).PaddingRight(1),

		inputBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(dim),

		statusBar: lipgloss.NewStyle().
			Foreground(dim).PaddingLeft(1),
	}
}
