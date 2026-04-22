package tui

import "testing"

func TestVisibleWidth(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"", 0},
		{"\x1b[31mred\x1b[0m", 3},           // ANSI color codes stripped
		{"\x1b]0;title\x07text", 4},           // OSC sequence stripped
		{"\x1b[1;31mbold red\x1b[0m", 8},      // multiple params
		{"日本語", 6},                            // CJK double-width
		{"\x1b[32m日本\x1b[0m", 4},              // ANSI + CJK
	} {
		if got := visibleWidth(tc.input); got != tc.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestShouldRerenderSubmittedSingleLine(t *testing.T) {
	// Fits: prompt(2) + input(3) = 5 <= 80
	if shouldRerenderSubmittedSingleLine("> ", "abc", 80) {
		t.Error("should not rerender when it fits")
	}
	// Overflows: prompt(2) + input(5) = 7 > 6
	if !shouldRerenderSubmittedSingleLine("> ", "hello", 6) {
		t.Error("should rerender when it overflows")
	}
	// Zero width.
	if shouldRerenderSubmittedSingleLine("> ", "x", 0) {
		t.Error("should not rerender with zero width")
	}
}

func TestTruncateToWidth(t *testing.T) {
	for _, tc := range []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 3, "hel"},
		{"hello", 10, "hello"},
		{"hello", 0, ""},
		{"日本語", 4, "日本"},   // each char is width 2
		{"日本語", 5, "日本"},   // 5 can't fit 3rd char (would be 6)
		{"ab日", 3, "ab"},     // 'ab'=2, '日'=2, total would be 4 > 3
	} {
		if got := truncateToWidth(tc.input, tc.max); got != tc.want {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func TestTruncateToWidthFromEnd(t *testing.T) {
	for _, tc := range []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 3, "llo"},
		{"hello", 10, "hello"},
		{"hello", 0, ""},
		{"日本語", 4, "本語"},
		{"ab日cd", 4, "日cd"},
	} {
		if got := truncateToWidthFromEnd(tc.input, tc.max); got != tc.want {
			t.Errorf("truncateToWidthFromEnd(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}
