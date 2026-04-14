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

// diffColorWriter wraps an io.Writer and colorizes diff lines within tool
// results. A tool result block begins on the line after a "-> toolname(...)"
// call line and ends at the next known-prefix chat line.
type diffColorWriter struct {
	out      io.Writer
	buf      []byte
	inResult bool
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
	// A tool call starts the next block as a result block.
	if strings.HasPrefix(line, "-> ") {
		w.inResult = true
		io.WriteString(w.out, line) //nolint:errcheck
		return
	}
	// Any other known block prefix ends result mode.
	if w.inResult && isBlockStart(line) {
		w.inResult = false
	}
	if w.inResult {
		w.writeDiffLine(line)
		return
	}
	io.WriteString(w.out, line) //nolint:errcheck
}

func (w *diffColorWriter) writeDiffLine(line string) {
	// file_edit emits ＋ (U+FF0B) and − (U+2212) as the leading byte of diff
	// lines so that only these explicit markers trigger coloring, not incidental
	// ASCII +/- in command output (e.g. ls -la permissions).
	switch {
	case strings.HasPrefix(line, "＋++") || strings.HasPrefix(line, "−--"):
		io.WriteString(w.out, ansiDim+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "＋"):
		io.WriteString(w.out, ansiGreen+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "−"):
		io.WriteString(w.out, ansiRed+line+ansiReset) //nolint:errcheck
	case strings.HasPrefix(line, "@@"):
		io.WriteString(w.out, ansiCyan+line+ansiReset) //nolint:errcheck
	default:
		io.WriteString(w.out, line) //nolint:errcheck
	}
}
