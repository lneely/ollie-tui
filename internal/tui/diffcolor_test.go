package tui

import (
	"bytes"
	"testing"
)

func dcw(input string) string {
	var buf bytes.Buffer
	w := newDiffColorWriter(&buf)
	w.Write([]byte(input))
	w.Flush()
	return buf.String()
}

func dcwChunked(chunks ...string) string {
	var buf bytes.Buffer
	w := newDiffColorWriter(&buf)
	for _, c := range chunks {
		w.Write([]byte(c))
	}
	w.Flush()
	return buf.String()
}

func TestDiffColor_PassthroughOutsideResult(t *testing.T) {
	input := "assistant: hello world\nuser: hi\n"
	if got := dcw(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestDiffColor_ColorizeDiffInResult(t *testing.T) {
	input := "-> edit(file.go)\n＋added line\n−removed line\n@@hunk@@\n"
	got := dcw(input)
	if got == input {
		t.Error("expected colorized output, got passthrough")
	}
	// Check ANSI codes are present for each diff prefix.
	for _, want := range []string{ansiGreen, ansiRed, ansiCyan} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("missing %q in output %q", want, got)
		}
	}
}

func TestDiffColor_DimHeaders(t *testing.T) {
	got := dcw("-> edit(f)\n＋++ a/file\n−-- b/file\n")
	for _, want := range []string{ansiDim, ansiReset} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("missing %q in output %q", want, got)
		}
	}
}

func TestDiffColor_ResultEndsAtBlockStart(t *testing.T) {
	// After a tool result, an "assistant: " line should NOT be colorized.
	input := "-> run(ls)\noutput\nassistant: thinking\n"
	got := dcw(input)
	// "assistant: thinking" should appear verbatim (no ANSI).
	if !bytes.Contains([]byte(got), []byte("assistant: thinking\n")) {
		t.Errorf("assistant line was colorized: %q", got)
	}
}

func TestDiffColor_PlainLinesInResultPassthrough(t *testing.T) {
	// Non-diff lines inside a result block should pass through without color.
	input := "-> run(cmd)\nplain output\nnext line\nassistant: done\n"
	got := dcw(input)
	if !bytes.Contains([]byte(got), []byte("plain output\n")) {
		t.Errorf("plain line modified: %q", got)
	}
}

func TestDiffColor_ChunkedWrite(t *testing.T) {
	// Split a line across two writes — should produce same output as single write.
	full := "-> edit(f)\n＋added\nassistant: ok\n"
	single := dcw(full)
	chunked := dcwChunked("-> edit(f)\n＋ad", "ded\nassistant: ok\n")
	if single != chunked {
		t.Errorf("chunked differs:\n  single:  %q\n  chunked: %q", single, chunked)
	}
}

func TestDiffColor_FlushPartialLine(t *testing.T) {
	var buf bytes.Buffer
	w := newDiffColorWriter(&buf)
	w.Write([]byte("assistant: partial"))
	// No newline yet — should be buffered.
	if buf.Len() != 0 {
		t.Errorf("expected empty output before flush, got %q", buf.String())
	}
	w.Flush()
	if got := buf.String(); got != "assistant: partial" {
		t.Errorf("after flush: got %q", got)
	}
}

func TestDiffColor_MultipleToolBlocks(t *testing.T) {
	input := "-> run(a)\nresult a\nassistant: mid\n-> run(b)\n＋diff b\nassistant: end\n"
	got := dcw(input)
	// "result a" should be plain (no diff prefix), "＋diff b" should be green.
	if bytes.Contains([]byte(got), []byte(ansiGreen+"result a")) {
		t.Error("result a should not be colorized")
	}
	if !bytes.Contains([]byte(got), []byte(ansiGreen+"＋diff b")) {
		t.Errorf("＋diff b should be green: %q", got)
	}
}
