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

func TestDiffColor_ColorizeDiffInFence(t *testing.T) {
	input := "-> edit(file.go)\n```diff\n+added line\n-removed line\n@@hunk@@\n```\n"
	got := dcw(input)
	if got == input {
		t.Error("expected colorized output, got passthrough")
	}
	for _, want := range []string{ansiGreen, ansiRed, ansiCyan} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("missing %q in output %q", want, got)
		}
	}
}

func TestDiffColor_DimHeaders(t *testing.T) {
	got := dcw("-> edit(f)\n```diff\n+++ a/file\n--- b/file\n```\n")
	for _, want := range []string{ansiDim, ansiReset} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("missing %q in output %q", want, got)
		}
	}
}

func TestDiffColor_NoDiffOutsideFence(t *testing.T) {
	// +/- outside a ```diff fence must not be colorized.
	input := "-> run(cmd)\n+not a diff\n-also not\nassistant: done\n"
	got := dcw(input)
	if bytes.Contains([]byte(got), []byte(ansiGreen)) {
		t.Errorf("+ outside diff fence was colorized: %q", got)
	}
	if bytes.Contains([]byte(got), []byte(ansiRed)) {
		t.Errorf("- outside diff fence was colorized: %q", got)
	}
}

func TestDiffColor_ResultEndsAtBlockStart(t *testing.T) {
	input := "-> run(ls)\noutput\nassistant: thinking\n"
	got := dcw(input)
	if !bytes.Contains([]byte(got), []byte("assistant: thinking\n")) {
		t.Errorf("assistant line was colorized: %q", got)
	}
}

func TestDiffColor_PlainLinesInResultPassthrough(t *testing.T) {
	input := "-> run(cmd)\nplain output\nnext line\nassistant: done\n"
	got := dcw(input)
	if !bytes.Contains([]byte(got), []byte("plain output\n")) {
		t.Errorf("plain line modified: %q", got)
	}
}

func TestDiffColor_ChunkedWrite(t *testing.T) {
	full := "-> edit(f)\n```diff\n+added\n```\nassistant: ok\n"
	single := dcw(full)
	chunked := dcwChunked("-> edit(f)\n```diff\n+ad", "ded\n```\nassistant: ok\n")
	if single != chunked {
		t.Errorf("chunked differs:\n  single:  %q\n  chunked: %q", single, chunked)
	}
}

func TestDiffColor_FlushPartialLine(t *testing.T) {
	var buf bytes.Buffer
	w := newDiffColorWriter(&buf)
	w.Write([]byte("assistant: partial"))
	if buf.Len() != 0 {
		t.Errorf("expected empty output before flush, got %q", buf.String())
	}
	w.Flush()
	if got := buf.String(); got != "assistant: partial" {
		t.Errorf("after flush: got %q", got)
	}
}

func TestDiffColor_MultipleToolBlocks(t *testing.T) {
	input := "-> run(a)\nresult a\nassistant: mid\n-> run(b)\n```diff\n+diff b\n```\nassistant: end\n"
	got := dcw(input)
	if bytes.Contains([]byte(got), []byte(ansiGreen+"result a")) {
		t.Error("result a should not be colorized")
	}
	if !bytes.Contains([]byte(got), []byte(ansiGreen+"+diff b")) {
		t.Errorf("+diff b should be green: %q", got)
	}
}
