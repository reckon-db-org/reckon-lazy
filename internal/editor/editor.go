// Package editor hands a payload off to the user's $EDITOR for
// read-only inspection. Bubble Tea's altscreen is suspended for the
// duration; control returns once the editor exits.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Inspect returns a tea.Cmd that, when fired, writes the payload to
// a cache file and runs $EDITOR on it. Bubble Tea suspends the
// altscreen for the duration via tea.ExecProcess so the editor takes
// over the terminal cleanly.
//
//   - name: human-readable filename stem (e.g. event id). Path-safed.
//   - ext: file extension without the dot (e.g. "json"). The editor
//     uses this to choose a syntax highlighter.
//   - payload: bytes to inspect (read-only — any writeback is ignored).
//
// The cache file persists under $XDG_CACHE_HOME/lazyreckon (defaults
// to ~/.cache/lazyreckon). Old files accumulate; that's fine for now.
func Inspect(name, ext string, payload []byte) tea.Cmd {
	path, err := writeCacheFile(name, ext, payload)
	if err != nil {
		return func() tea.Msg { return DoneMsg{Err: err} }
	}

	cmd := exec.Command(editorBinary(), path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return DoneMsg{Err: err, Path: path}
	})
}

// DoneMsg is delivered after the editor exits.
type DoneMsg struct {
	Path string
	Err  error
}

// editorBinary picks the editor to spawn. Order: $EDITOR, $VISUAL,
// nvim, vim, nano. Falls back to `less` so we always have *something*.
func editorBinary() string {
	for _, candidate := range []string{
		os.Getenv("EDITOR"),
		os.Getenv("VISUAL"),
		"nvim", "vim", "nano",
	} {
		if candidate == "" {
			continue
		}
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return "less"
}

func writeCacheFile(name, ext string, payload []byte) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	safe := safeName(name)
	if ext != "" {
		safe += "." + ext
	}
	path := filepath.Join(dir, safe)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func cacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "lazyreckon"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}
	return filepath.Join(home, ".cache", "lazyreckon"), nil
}

// safeName replaces filesystem-unfriendly characters in s so the
// cache filename is portable. Doesn't try to be clever about Unicode
// — we just need something a shell + editor will tolerate.
func safeName(s string) string {
	if s == "" {
		return "untitled"
	}
	repl := strings.NewReplacer(
		"/", "_", "\\", "_", " ", "_", ":", "_",
		"?", "_", "*", "_", "<", "_", ">", "_", "|", "_",
	)
	out := repl.Replace(s)
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
