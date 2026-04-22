package tui

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// lineReader wraps a ReadSeeker and only returns bytes up to (and including)
// the last newline — simulating a 9P server that buffers partial lines.
type lineReader struct {
	r io.ReadSeeker
}

func (lr *lineReader) Read(p []byte) (int, error) {
	n, err := lr.r.Read(p)
	if n == 0 {
		return n, err
	}
	// Find last newline in the chunk; truncate after it.
	last := bytes.LastIndexByte(p[:n], '\n')
	if last < 0 {
		// No newline at all — return nothing, seek back.
		lr.r.Seek(-int64(n), io.SeekCurrent)
		return 0, nil
	}
	// Seek back the bytes after the last newline.
	trim := n - (last + 1)
	if trim > 0 {
		lr.r.Seek(-int64(trim), io.SeekCurrent)
	}
	return last + 1, err
}

func (lr *lineReader) Seek(offset int64, whence int) (int64, error) {
	return lr.r.Seek(offset, whence)
}

// simulateTail mimics the tailUntilIdle read loop: read from file at offset,
// feed through diffColorWriter, advance offset, then flush at the end.
// Returns the output written and the new offset.
func simulateTail(path string, offset int64, out io.Writer) int64 {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	f.Seek(offset, io.SeekStart)

	buf := make([]byte, 4096)
	cw := newDiffColorWriter(out)
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			break
		}
		cw.Write(buf[:n])
		offset += int64(n)
	}
	cw.Flush()
	return offset
}

func TestTailWithTrailingNewline(t *testing.T) {
	tmp := t.TempDir()
	chat := tmp + "/chat"

	// Turn 1: server writes user prompt + assistant response, WITH trailing newline.
	os.WriteFile(chat, []byte("user: hello\nassistant: hi there\n"), 0644)

	var out bytes.Buffer
	off := simulateTail(chat, 0, &out)

	if got := out.String(); got != "user: hello\nassistant: hi there\n" {
		t.Fatalf("turn 1: got %q", got)
	}
	t.Logf("turn 1 offset: %d", off)

	// Turn 2: server appends next exchange.
	f, _ := os.OpenFile(chat, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("user: how are you\nassistant: good\n")
	f.Close()

	out.Reset()
	off = simulateTail(chat, off, &out)

	if got := out.String(); got != "user: how are you\nassistant: good\n" {
		t.Fatalf("turn 2: got %q", got)
	}
	t.Logf("turn 2 offset: %d", off)
}

// simulateTail9P is like simulateTail but uses a lineReader that only
// returns complete lines — modeling a 9P server that withholds partial lines.
func simulateTail9P(path string, offset int64, out io.Writer) int64 {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	f.Seek(offset, io.SeekStart)

	lr := &lineReader{r: f}
	buf := make([]byte, 4096)
	cw := newDiffColorWriter(out)
	for {
		n, _ := lr.Read(buf)
		if n == 0 {
			break
		}
		cw.Write(buf[:n])
		offset += int64(n)
	}
	cw.Flush()
	return offset
}

func TestTailWithoutTrailingNewline(t *testing.T) {
	tmp := t.TempDir()
	chat := tmp + "/chat"

	// Turn 1: server writes user prompt + assistant response, NO trailing newline.
	os.WriteFile(chat, []byte("user: hello\nassistant: hi there"), 0644)

	var out bytes.Buffer
	off := simulateTail(chat, 0, &out)

	// Does the output contain the full assistant response?
	if got := out.String(); got != "user: hello\nassistant: hi there" {
		t.Fatalf("turn 1: got %q", got)
	}
	t.Logf("turn 1 offset: %d", off)

	// Turn 2: server appends next exchange (starts with \n from previous line).
	f, _ := os.OpenFile(chat, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("\nuser: how are you\nassistant: good")
	f.Close()

	out.Reset()
	off = simulateTail(chat, off, &out)

	// KEY CHECK: does turn 2 output contain ONLY turn 2 content,
	// or does it also contain leftover from turn 1?
	got := out.String()
	t.Logf("turn 2 output: %q", got)

	if got != "\nuser: how are you\nassistant: good" {
		t.Errorf("turn 2: unexpected output %q", got)
	}
}

// TestTail9PWithoutTrailingNewline simulates a 9P server that only returns
// complete lines. This demonstrates the "one step behind" bug when the chat
// file lacks a trailing newline. The core fix (EnsureTrailingNewline) prevents
// this scenario; this test documents the failure mode.
func TestTail9PWithoutTrailingNewline(t *testing.T) {
	t.Skip("documents the bug; core fix ensures trailing newline so this path no longer occurs")
	tmp := t.TempDir()
	chat := tmp + "/chat"

	// Turn 1: assistant response has NO trailing newline.
	os.WriteFile(chat, []byte("user: hello\nassistant: hi there"), 0644)

	var out bytes.Buffer
	off := simulateTail9P(chat, 0, &out)

	t.Logf("turn 1 output: %q", out.String())
	t.Logf("turn 1 offset: %d (file size: 31)", off)

	// The line-buffered reader won't return "assistant: hi there" because
	// it has no trailing newline. Offset stops at the last newline.
	if off == 31 {
		t.Log("turn 1: offset reached end of file — no lag expected")
	} else {
		t.Logf("turn 1: offset stopped at %d — %d bytes unread (LAGGING)", off, 31-off)
	}

	// Turn 2: server appends next exchange.
	f, _ := os.OpenFile(chat, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("\nuser: how are you\nassistant: good")
	f.Close()

	out.Reset()
	off = simulateTail9P(chat, off, &out)

	got := out.String()
	t.Logf("turn 2 output: %q", got)

	// If lagging, turn 2 output will START with the previous assistant
	// response ("assistant: hi there") glued to the new user prompt.
	if bytes.Contains([]byte(got), []byte("hi there")) {
		t.Error("BUG CONFIRMED: turn 2 contains turn 1's assistant response — one step behind")
	}
}

// TestTail9PWithTrailingNewline verifies the fix: when the server ensures
// a trailing newline after each turn, the 9P line-buffered reader works correctly.
func TestTail9PWithTrailingNewline(t *testing.T) {
	tmp := t.TempDir()
	chat := tmp + "/chat"

	// Turn 1: assistant response WITH trailing newline (the fix).
	os.WriteFile(chat, []byte("user: hello\nassistant: hi there\n"), 0644)

	var out bytes.Buffer
	off := simulateTail9P(chat, 0, &out)

	if got := out.String(); got != "user: hello\nassistant: hi there\n" {
		t.Fatalf("turn 1: got %q", got)
	}
	if off != 32 {
		t.Fatalf("turn 1: offset %d, want 32", off)
	}

	// Turn 2: server appends next exchange (also newline-terminated).
	f, _ := os.OpenFile(chat, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString("user: how are you\nassistant: good\n")
	f.Close()

	out.Reset()
	off = simulateTail9P(chat, off, &out)

	got := out.String()
	if got != "user: how are you\nassistant: good\n" {
		t.Errorf("turn 2: got %q", got)
	}
	if bytes.Contains([]byte(got), []byte("hi there")) {
		t.Error("turn 2 contains turn 1 content — still lagging")
	}
}
