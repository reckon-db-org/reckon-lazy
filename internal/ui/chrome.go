// Package ui renders the cross-cutting screen elements: header,
// tab strip, status bar. Panes (under internal/panes) are the
// content; this is the chrome.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// Header renders the top banner: glyph + wordmark + endpoint, padded
// to width.
func Header(endpoint string, width int) string {
	mark := theme.HeaderAccent.Render(theme.Glyph) +
		theme.HeaderBar.Render(" lazyreckon")
	right := theme.HeaderBar.Render(endpoint)

	gap := width - lipgloss.Width(mark) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	filler := theme.HeaderBar.Render(strings.Repeat(" ", gap))
	return mark + filler + right
}

// Tabs renders the labelled tab strip. active is the index into labels.
func Tabs(labels []string, active, width int) string {
	parts := make([]string, 0, len(labels))
	for i, label := range labels {
		text := " " + label + " "
		if i == active {
			parts = append(parts, theme.TabActive.Render(text))
		} else {
			parts = append(parts, theme.TabInactive.Render(text))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	gap := width - lipgloss.Width(row)
	if gap < 0 {
		gap = 0
	}
	return row + theme.TabGap.Render(strings.Repeat(" ", gap))
}

// StatusBar renders the bottom bar with keymap hints + a right-aligned
// summary (e.g. cluster health).
func StatusBar(keys []KeyHint, summary string, width int) string {
	var b strings.Builder
	for i, h := range keys {
		if i > 0 {
			b.WriteString(theme.StatusBar.Render("  "))
		}
		b.WriteString(theme.StatusKey.Render(h.Key))
		b.WriteString(theme.StatusBar.Render(" " + h.Action))
	}
	left := b.String()
	right := theme.StatusBar.Render(summary)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + theme.StatusBar.Render(strings.Repeat(" ", gap)) + right
}

// KeyHint is one "key → action" pair shown in the status bar.
type KeyHint struct {
	Key    string
	Action string
}
