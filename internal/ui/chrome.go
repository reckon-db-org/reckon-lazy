// Package ui renders the cross-cutting screen elements: header,
// mode strip, status bar. The body (three-column ranger layout)
// lives under internal/ranger.
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Header renders the persistent top banner: glyph + wordmark + active
// store + cluster health + endpoint. Padded to width.
func Header(endpoint string, store string, h Health, width int) string {
	mark := theme.HeaderAccent.Render(theme.Glyph) +
		theme.HeaderBar.Render(" lazyreckon ")

	storeChip := theme.HeaderBar.Render("· store ") +
		theme.HeaderAccent.Render(store) + theme.HeaderBar.Render(" ")

	healthChip := theme.HeaderBar.Render("· ") + healthBadge(h)

	right := theme.HeaderBar.Render(endpoint + " ")

	left := mark + storeChip + healthChip
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + theme.HeaderBar.Render(strings.Repeat(" ", gap)) + right
}

// Health is what the header needs to render the cluster status chip.
type Health struct {
	NodesUp    int
	NodesTotal int
	Leader     string
	OK         bool
}

func healthBadge(h Health) string {
	if h.NodesTotal == 0 {
		return theme.StatusDim.Inline(true).Render("connecting…")
	}
	dot := theme.BadgeOK.Inline(true).Render("●")
	if !h.OK {
		dot = theme.BadgeError.Inline(true).Render("●")
	}
	text := fmt.Sprintf(" %d/%d", h.NodesUp, h.NodesTotal)
	out := theme.HeaderBar.Render(dot) + theme.HeaderBar.Render(text)
	if h.Leader != "" {
		out += theme.HeaderBar.Render(" lead ") +
			theme.HeaderAccent.Render(shortNode(h.Leader)) +
			theme.HeaderBar.Render(" ")
	} else {
		out += theme.HeaderBar.Render(" ")
	}
	return out
}

func shortNode(s string) string {
	// reckon_gateway@192.168.1.12 → .12
	if i := strings.LastIndex(s, "."); i >= 0 {
		return "." + s[i+1:]
	}
	return s
}

// ModeStrip renders the bottom mode selector. Active mode shows in
// Horus green; others muted. On a narrow terminal labels collapse
// to "1 2 3 4" (number-only) so the strip always fits one line.
func ModeStrip(labels []string, active, width int) string {
	row := renderModeStrip(labels, active, false)
	if lipgloss.Width(row) > width {
		row = renderModeStrip(labels, active, true)
	}
	// Hard-truncate as a last resort (very small terminals).
	if w := lipgloss.Width(row); w > width {
		return row // let it overflow rather than break ANSI codes mid-byte
	}
	gap := width - lipgloss.Width(row)
	return row + theme.TabGap.Render(strings.Repeat(" ", gap))
}

func renderModeStrip(labels []string, active int, compact bool) string {
	parts := make([]string, 0, len(labels))
	for i, label := range labels {
		var text string
		if compact {
			text = fmt.Sprintf(" %d ", i+1)
			_ = label
		} else {
			text = fmt.Sprintf(" %d %s ", i+1, label)
		}
		if i == active {
			parts = append(parts, theme.TabActive.Render(text))
		} else {
			parts = append(parts, theme.TabInactive.Render(text))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// StatusBar renders the bottom-most line: keymap hints + right-aligned
// summary (e.g. clock). Drops hints from the right (lowest priority
// last) until everything fits one line — never wraps.
func StatusBar(keys []KeyHint, summary string, width int) string {
	right := theme.StatusBar.Render(summary)
	rightW := lipgloss.Width(right)

	left, leftW := renderHints(keys)

	// Drop hints from the end until left + 1 (gap) + right fits.
	for len(keys) > 0 && leftW+1+rightW > width {
		keys = keys[:len(keys)-1]
		left, leftW = renderHints(keys)
	}

	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + theme.StatusBar.Render(strings.Repeat(" ", gap)) + right
}

func renderHints(keys []KeyHint) (string, int) {
	var b strings.Builder
	for i, h := range keys {
		if i > 0 {
			b.WriteString(theme.StatusBar.Render("  "))
		}
		b.WriteString(theme.StatusKey.Render(h.Key))
		b.WriteString(theme.StatusBar.Render(" " + h.Action))
	}
	s := b.String()
	return s, lipgloss.Width(s)
}

// KeyHint is one "key → action" pair shown in the status bar.
type KeyHint struct {
	Key    string
	Action string
}
