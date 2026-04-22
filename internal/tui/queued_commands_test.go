package tui

import "testing"

func TestParseQueuedCommandLine(t *testing.T) {
	for _, tc := range []struct {
		input   string
		ok      bool
		wantErr bool
		action  queuedCommandAction
		count   int
	}{
		{"hello", false, false, 0, 0},
		{"/other", false, false, 0, 0},
		{"/queued", true, false, queuedCommandHelp, 0},
		{"/queued pop", true, false, queuedCommandPop, 1},
		{"/queued pop 5", true, false, queuedCommandPop, 5},
		{"/queued clear", true, false, queuedCommandClear, 0},
		{"/queued pop -1", true, true, 0, 0},
		{"/queued pop x", true, true, 0, 0},
		{"/queued pop 1 2", true, true, 0, 0},
		{"/queued clear x", true, true, 0, 0},
		{"/queued bogus", true, true, 0, 0},
	} {
		cmd, ok, err := parseQueuedCommandLine(tc.input)
		if ok != tc.ok {
			t.Errorf("%q: ok=%v, want %v", tc.input, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.input, err)
			continue
		}
		if cmd.action != tc.action {
			t.Errorf("%q: action=%v, want %v", tc.input, cmd.action, tc.action)
		}
		if cmd.count != tc.count {
			t.Errorf("%q: count=%d, want %d", tc.input, cmd.count, tc.count)
		}
	}
}

func TestRunQueuedCommand(t *testing.T) {
	q := []string{"a", "b", "c"}

	// Pop 1 removes last.
	got, msg := runQueuedCommand(q, queuedCommand{action: queuedCommandPop, count: 1})
	if len(got) != 2 || got[1] != "b" {
		t.Errorf("pop 1: got %v", got)
	}
	if msg == "" {
		t.Error("pop 1: empty message")
	}

	// Pop more than available.
	got, _ = runQueuedCommand(q, queuedCommand{action: queuedCommandPop, count: 10})
	if len(got) != 0 {
		t.Errorf("pop 10: got %v, want empty", got)
	}

	// Pop from empty.
	got, msg = runQueuedCommand(nil, queuedCommand{action: queuedCommandPop, count: 1})
	if got != nil {
		t.Errorf("pop empty: got %v", got)
	}
	if msg != "No queued prompts." {
		t.Errorf("pop empty: msg=%q", msg)
	}

	// Clear.
	got, _ = runQueuedCommand(q, queuedCommand{action: queuedCommandClear})
	if got != nil {
		t.Errorf("clear: got %v", got)
	}

	// Help.
	got, msg = runQueuedCommand(q, queuedCommand{action: queuedCommandHelp})
	if len(got) != 3 {
		t.Errorf("help: queue modified: %v", got)
	}
	if msg == "" {
		t.Error("help: empty message")
	}
}
