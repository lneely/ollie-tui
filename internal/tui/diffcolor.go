package tui

import (
	"bytes"
	"io"
	"strings"
)

const (
	ansiReset = "\033[0m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiCyan  = "\033[36m"
	ansiDim   = "\033[2m"
)

// diffColorWriter wraps an io.Writer and colorizes diff lines within ```diff
// fenced blocks in tool results. Coloring is scoped strictly to the fence —
// incidental +/- outside a diff block are passed through unchanged.
type diffColorWriter struct {
	out         io.Writer
	buf         []byte
	inResult    bool
	inDiffBlock bool
}

func newDiffColorWriter(out io.Writer) *diffColorWriter {
	return &diffColorWriter{out: out}
}

func (w *diffColorWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i+1])
		w.buf = w.buf[i+1:]
		w.writeLine(line)
	}
	return len(p), nil
}

// Flush writes any buffered content that has not yet ended in a newline.
func (w *diffColorWriter) Flush() {
	if len(w.buf) > 0 {
		w.writeLine(string(w.buf))
		w.buf = nil
	}
}

// isBlockStart returns true for lines that start a new named chat block.
func isBlockStart(line string) bool {
	return strings.HasPrefix(line, "-> ") ||
		strings.HasPrefix(line, "user: ") ||
		strings.HasPrefix(line, "assistant: ") ||
		strings.HasPrefix(line, "retrying ") ||
		strings.HasPrefix(line, "error: ") ||
		strings.HasPrefix(line, "agent stalled")
}

func (w *diffColorWriter) writeLine(line string) {
	if strings.HasPrefix(line, "-> ") {
		w.inResult = true
		w.inDiffBlock = false
		io.WriteString(w.out, line) //nolint:errcheck
		return
	}
	if w.inResult && isBlockStart(line) {
		w.inResult = false
		w.inDiffBlock = false
	}
	if w.inResult {
		stripped := strings.TrimRight(line, "\n")
		if stripped == "```diff" {
			w.inDiffBlock = true
			io.WriteString(w.out, line) //nolint:errcheck
			return
		}
		if w.inDiffBlock && stripped == "```" {
			w.inDiffBlock = false
			io.WriteString(w.out, line) //nolint:errcheck
			return
		}
		if w.inDiffBlock {
			w.writeDiffLine(line)
			return
		}
	}
	io.WriteString(w.out, line) //nolint:errcheck
}

func (w *diffColorWriter) writeDiffLine(line string) {
	switch {
	case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		io.WriteString(w.out, ansiDim+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "+"):
		io.WriteString(w.out, ansiGreen+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "-"):
		io.WriteString(w.out, ansiRed+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "@@"):
		io.WriteString(w.out, ansiCyan+line+ansiReset) //nolint:errcheck
	default:
		io.WriteString(w.out, line) //nolint:errcheck
	}
}
