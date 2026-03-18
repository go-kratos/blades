package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// ── View ─────────────────────────────────────────────────────────────────────

func (m *model) View() tea.View {
	var content string
	if m.quitting {
		content = m.styles.dim.Render("Bye! 👋") + "\n"
	} else if !m.vpReady {
		content = ""
	} else {
		content = m.vp.View() + "\n" +
			m.statusBar() + "\n" +
			m.styles.inputBox.Width(m.width-2).Render(m.input.View()) + "\n"
	}

	v := tea.NewView(content)
	v.AltScreen = true

	// Provide real cursor position so ConPTY/IME can track the input caret.
	// Cursor() returns nil when unfocused or when virtual cursor is in use.
	if m.vpReady && m.state == stateInput {
		if c := m.input.Cursor(); c != nil {
			// The bubbles library computes X as rune index + prompt width,
			// but CJK characters occupy 2 terminal columns per rune.
			// Add the extra columns from wide characters before the cursor.
			pos := m.input.Position()
			val := []rune(m.input.Value())
			if pos > len(val) {
				pos = len(val)
			}
			wideDelta := uniseg.StringWidth(string(val[:pos])) - pos

			c.Position.X += 2 + wideDelta
			c.Position.Y += m.vpHeight() + 2

			// Clamp to the visible input area to handle horizontal scroll.
			promptWidth := lipgloss.Width(m.input.Prompt)
			if maxX := m.input.Width() + promptWidth + 2; c.Position.X > maxX {
				c.Position.X = maxX
			}

			v.Cursor = c
		}
	}

	return v
}

func (m *model) statusBar() string {
	if m.state == stateRunning {
		return m.styles.statusBar.Render(m.spinner.View() + " thinking...  Esc to stop")
	}
	if m.err != nil {
		return m.styles.statusBar.Render(m.styles.err.Render("error: " + m.err.Error()))
	}
	for i := len(m.turns) - 1; i >= 0; i-- {
		if !m.turns[i].isMeta {
			if len(m.turns[i].tools) > 0 {
				return m.styles.statusBar.Render(m.styles.hint.Render("1-9 details  PgUp/PgDn scroll  /help"))
			}
			break
		}
	}
	return m.styles.statusBar.Render(m.styles.hint.Render("PgUp/PgDn scroll  /help"))
}

// ── Content construction ──────────────────────────────────────────────────────

// vpHeight returns the height available for the viewport.
func (m *model) vpHeight() int {
	h := m.height - 5
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) refreshViewport(scrollToBottom bool) {
	if !m.vpReady {
		return
	}
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(m.buildContent())
	if scrollToBottom || atBottom {
		m.vp.GotoBottom()
	}
}

// rebuildPastContent re-renders all completed turns into a cached string.
// Call this whenever turns are added/modified, but NOT on every streaming token.
func (m *model) rebuildPastContent() {
	var b strings.Builder

	b.WriteString(m.styles.header.Render("blades"))
	b.WriteString("  ")
	b.WriteString(m.styles.dim.Render("session " + m.sessionID))
	b.WriteString("\n\n")

	for _, t := range m.turns {
		b.WriteString(m.renderTurn(t))
		b.WriteString("\n")
	}
	m.pastContent = b.String()
}

// buildContent assembles the full viewport content: past turns + streaming section.
func (m *model) buildContent() string {
	if m.state == stateRunning {
		return m.pastContent + m.renderStreaming()
	}
	return m.pastContent
}

func (m *model) renderTurn(t *convTurn) string {
	if t.isMeta {
		return t.metaText
	}
	var b strings.Builder
	b.WriteString(m.styles.userLabel.Render("User"))
	b.WriteString(" ")
	b.WriteString(t.user)
	b.WriteString("\n")
	if t.rendered != "" || len(t.tools) > 0 {
		b.WriteString(m.styles.assistLabel.Render("Assistant"))
		b.WriteString("\n")
		if t.rendered != "" {
			b.WriteString(t.rendered)
			if len(t.tools) > 0 {
				b.WriteString("\n")
			}
		}
		for i := range t.tools {
			b.WriteString(m.renderToolSection(&t.tools[i]))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *model) renderStreaming() string {
	var b strings.Builder
	if m.streamBuf.Len() > 0 || len(m.streamTools) > 0 {
		b.WriteString(m.styles.assistLabel.Render("Assistant"))
		b.WriteString("\n")
		if m.streamBuf.Len() > 0 {
			b.WriteString(m.streamBuf.String())
			if len(m.streamTools) > 0 {
				b.WriteString("\n")
			}
		}
		for i := range m.streamTools {
			b.WriteString(m.renderToolSection(&m.streamTools[i]))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *model) renderToolSection(ts *toolSection) string {
	maxW := m.width - 6
	if maxW < 20 {
		maxW = 20
	}
	label := m.styles.toolLabel.Render(fmt.Sprintf("%d. %s", ts.idx, ts.name))

	if !ts.complete {
		content := label + "  " + m.styles.dim.Render("running")
		if ts.input != "" {
			if ts.expanded {
				content += "\n" + m.styles.dim.Render("input: ") + ts.input
			} else {
				content += "\n" + m.styles.dim.Render(truncate(singleLine(ts.input), maxW-4))
			}
		}
		if ts.expanded {
			return m.styles.toolBoxActive.Width(maxW).Render(content)
		}
		return m.styles.toolBox.Width(maxW).Render(content)
	}

	if !ts.expanded {
		preview := ""
		if ts.output != "" {
			preview = "\n" + m.styles.dim.Render(truncate(singleLine(ts.output), maxW-4))
		}
		return m.styles.toolBox.Width(maxW).Render(label + preview)
	}

	var content strings.Builder
	content.WriteString(label)
	if ts.input != "" {
		content.WriteString("\n")
		content.WriteString(m.styles.dim.Render("input: "))
		content.WriteString(ts.input)
	}
	if ts.output != "" {
		content.WriteString("\n")
		content.WriteString(m.styles.dim.Render("output: "))
		content.WriteString(ts.output)
	}
	return m.styles.toolBoxActive.Width(maxW).Render(content.String())
}

// toggleLastTool toggles the tool at 0-based toolIdx in the most recent real turn.
func (m *model) toggleLastTool(toolIdx int) bool {
	for i := len(m.turns) - 1; i >= 0; i-- {
		if !m.turns[i].isMeta {
			if toolIdx < len(m.turns[i].tools) {
				m.turns[i].tools[toolIdx].expanded = !m.turns[i].tools[toolIdx].expanded
				return true
			}
			return false
		}
	}
	return false
}

func (m *model) addMeta(md string) {
	t := &convTurn{isMeta: true}
	if m.glamour != nil {
		if rendered, err := m.glamour.Render(md); err == nil {
			t.metaText = rendered
		} else {
			t.metaText = md
		}
	} else {
		t.metaText = md
	}
	m.turns = append(m.turns, t)
}

func (m *model) addMetaRaw(s string) {
	m.turns = append(m.turns, &convTurn{isMeta: true, metaText: s})
}

// ── Utility ───────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if max <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func singleLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}
