// Package modes hosts one ranger triple per top-level mode.
// Each constructor returns a *ranger.Ranger already wired against
// an open reckon.Client and a chosen store.
package modes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"codeberg.org/reckon-db-org/reckon-lazy/internal/theme"
)

const shortRPCTimeout = 5 * time.Second

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w < 2 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

// windowAround returns [start, end) bounds for a viewport of height h
// scrolled so sel stays roughly centred when possible.
func windowAround(sel, n, h int) (int, int) {
	if n <= h {
		return 0, n
	}
	start := sel - h/2
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > n {
		end = n
		start = end - h
	}
	return start, end
}

// renderList highlights selected; arrow + Horus colour when active.
func renderList(items []string, selected, w, h int, active bool) string {
	if h < 1 {
		h = 1
	}
	start, end := windowAround(selected, len(items), h)
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		row := truncate(items[i], w-2)
		switch {
		case i == selected && active:
			out = append(out, theme.BadgeOK.Render("▸ ")+theme.RowKey.Render(row))
		case i == selected:
			out = append(out, theme.RowDim.Render("▸ ")+theme.RowValue.Render(row))
		default:
			out = append(out, "  "+theme.RowValue.Render(row))
		}
	}
	return strings.Join(out, "\n")
}

// prettyJSON returns indented JSON, soft-wrapping over-long lines to
// fit width w. Falls back to raw bytes if data isn't JSON.
func prettyJSON(raw []byte, w int) string {
	if len(raw) == 0 {
		return "(empty)"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	lines := strings.Split(string(out), "\n")
	for i, ln := range lines {
		if w > 4 && len(ln) > w {
			lines[i] = ln[:w-1] + "…"
		}
	}
	return strings.Join(lines, "\n")
}

// clip cuts s to at most h lines.
func clip(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// emptyHint — uniform placeholder for "select something to populate me".
func emptyHint(text string) string {
	return theme.RowDim.Render(text)
}

// errLine — uniform render for an error payload.
func errLine(err error) string {
	return theme.BadgeError.Render("error: ") + theme.RowValue.Render(err.Error())
}

// kvLine renders a key/value pair on its own line.
func kvLine(k, v string) string {
	return theme.RowHeader.Render(fmt.Sprintf("%-9s", k)) + " " + theme.RowValue.Render(v)
}

// padRight pads s with spaces to exactly w runes (truncates with …
// when too long).
func padRight(s string, w int) string {
	if len(s) >= w {
		if w > 1 {
			return s[:w-1] + "…"
		}
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// humanAgo renders a duration since t in a short form.
func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}
