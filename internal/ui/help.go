package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

// HelpSection groups related key bindings for one part of the UI.
type HelpSection struct {
	Title    string
	Bindings []HelpBinding
}

// HelpBinding is one key+description row in the cheatsheet.
type HelpBinding struct {
	Keys string
	What string
}

// HelpOverlay renders a centred modal showing the bindings for the
// current mode. width/height are the outer terminal dimensions; the
// modal sizes itself based on content + a sensible cap.
func HelpOverlay(modeLabel string, sections []HelpSection, width, height int) string {
	title := theme.PaneTitle.Render("◉  lazyreckon — keys (mode: " + modeLabel + ")")

	rows := []string{title, ""}
	for i, sec := range sections {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, theme.RowHeader.Render(sec.Title))
		for _, b := range sec.Bindings {
			row := "  " +
				theme.BadgeOK.Render(padCols(b.Keys, 12)) +
				"  " +
				theme.RowValue.Render(b.What)
			rows = append(rows, row)
		}
	}
	rows = append(rows, "",
		theme.RowDim.Render("press ? again or esc to dismiss"))

	body := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.HorusBright).
		Background(theme.VioletDeep).
		Padding(1, 3).
		Render(body)

	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// padCols pads s to exactly w runes. Avoids importing modes.padRight
// from a styling layer.
func padCols(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
