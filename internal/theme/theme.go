// Package theme is the lipgloss palette + named styles for lazyreckon.
//
// Colors are pulled from the reckon-portal artwork (Seshat, eye-of-horus,
// sphere). The visual identity: deep cosmic violet base, Horus acid-green
// for active/accent, Seshat gold for caution, sienna for danger. White on
// violet reads well on every terminal palette I've tested.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette — raw artwork colors. Use the named Styles below in views;
// reach for the palette only when composing one-off styles.
var (
	// Deep cosmic violet gradient (sphere background)
	VioletDeep  = lipgloss.Color("#1E0A2E")
	VioletDark  = lipgloss.Color("#320B4F")
	VioletMid   = lipgloss.Color("#3F125B")
	VioletLight = lipgloss.Color("#4C1D95")

	// Horus eye — acid green, the focus/accent color
	HorusBright = lipgloss.Color("#B8E234")
	HorusDeep   = lipgloss.Color("#9BCF20")

	// Seshat gold — caution, also good for highlighting numbers
	SeshatGold = lipgloss.Color("#fac53a")
	SeshatWarm = lipgloss.Color("#f5ca44")

	// Sienna — danger / failure
	Sienna = lipgloss.Color("#c13c1b")

	// Neutrals
	Ink    = lipgloss.Color("#FFFFFF")
	Dim    = lipgloss.Color("#666666")
	Muted  = lipgloss.Color("#4A4A4A")
	Shadow = lipgloss.Color("#000000")
)

// Styles — named, reusable. Build views by composing these; avoid
// inventing styles inline so the palette stays coherent.
var (
	// Branded header banner: violet bg, ink text, with a Horus accent
	// for the wordmark.
	HeaderBar = lipgloss.NewStyle().
			Background(VioletLight).
			Foreground(Ink).
			Padding(0, 2).
			Bold(true)

	HeaderAccent = lipgloss.NewStyle().
			Background(VioletLight).
			Foreground(HorusBright).
			Bold(true)

	// Tab strip
	TabActive = lipgloss.NewStyle().
			Background(VioletDark).
			Foreground(HorusBright).
			Padding(0, 2).
			Bold(true)

	TabInactive = lipgloss.NewStyle().
			Background(VioletDeep).
			Foreground(Dim).
			Padding(0, 2)

	TabGap = lipgloss.NewStyle().
		Background(VioletDeep)

	// Pane body
	Pane = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(VioletMid).
		Padding(0, 1)

	PaneTitle = lipgloss.NewStyle().
			Foreground(HorusBright).
			Bold(true).
			MarginBottom(1)

	// Table-row helpers
	RowHeader = lipgloss.NewStyle().
			Foreground(HorusDeep).
			Bold(true)

	RowKey = lipgloss.NewStyle().
		Foreground(Ink).
		Bold(true)

	RowValue = lipgloss.NewStyle().
			Foreground(Ink)

	RowDim = lipgloss.NewStyle().
		Foreground(Dim)

	// Status badges
	BadgeOK = lipgloss.NewStyle().
		Foreground(HorusBright).
		Bold(true)

	BadgeWarn = lipgloss.NewStyle().
			Foreground(SeshatGold).
			Bold(true)

	BadgeError = lipgloss.NewStyle().
			Foreground(Sienna).
			Bold(true)

	// Status bar at the bottom
	StatusBar = lipgloss.NewStyle().
			Background(VioletDark).
			Foreground(Ink).
			Padding(0, 1)

	StatusKey = lipgloss.NewStyle().
			Background(VioletDark).
			Foreground(HorusBright).
			Bold(true)

	StatusDim = lipgloss.NewStyle().
			Background(VioletDark).
			Foreground(Dim)
)

// Glyph — small symbolic eye-of-horus / sphere mark for the wordmark.
// One Unicode codepoint, terminal-safe.
const Glyph = "◉"
