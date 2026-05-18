// viewinspect renders the stores-mode body directly (no bubbletea)
// and prints the width of each row + each ranger's output, so we can
// see column-width misalignment without a real TTY.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	reckon "codeberg.org/reckon-db-org/reckon-go"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/cluster"
	"codeberg.org/reckon-db-org/reckon-lazy/internal/modes"
)

func main() {
	endpoint := flag.String("endpoint", "192.168.1.11:50051", "gateway")
	w := flag.Int("w", 187, "terminal width")
	h := flag.Int("h", 47, "body height (terminal_h - chrome)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := reckon.Connect(ctx, *endpoint)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer c.Close()

	topo := cluster.New()
	sv := modes.BuildStores(c, topo, "default_store", func(string) {})

	// Trigger Init so the columns load. Doesn't matter much for width
	// inspection — empty content still has the column-box shape.
	_ = sv.Init()
	time.Sleep(500 * time.Millisecond)

	body := sv.View(*w, *h)
	lines := strings.Split(body, "\n")
	fmt.Printf("body: %d lines (want %d)\n", len(lines), *h)
	widths := map[int]int{}
	for _, ln := range lines {
		widths[lipgloss.Width(ln)]++
	}
	fmt.Printf("distinct row widths: %v\n", widths)
	// Show the border-char positions on key rows: top row of top
	// ranger, bottom row of top ranger, top row of bot ranger, bottom row.
	for i, ln := range lines {
		fmt.Printf("  row %2d: w=%d %s\n", i, lipgloss.Width(ln), borderPositions(ln))
	}
}

// borderPositions returns a string showing where the box-drawing
// characters appear on this row, by visible column index.
func borderPositions(line string) string {
	plain := stripANSI(line)
	out := make([]rune, 0, len(plain))
	for _, r := range plain {
		switch r {
		case '╭', '╮', '╰', '╯', '│', '─':
			out = append(out, r)
		default:
			out = append(out, ' ')
		}
	}
	return string(out)
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= '@' && r <= '~') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
