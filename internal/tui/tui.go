package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	multiline "github.com/hymkor/go-multiline-ny"
	gotty "github.com/mattn/go-tty"
	readline "github.com/nyaosorg/go-readline-ny"

	"ollie-tui/internal/session"
)

const ctrlCExitWindow = 2 * time.Second

// recentHistory implements readline.IHistory for the multiline editor.
type recentHistory struct {
	entries []string
}

var _ readline.IHistory = (*recentHistory)(nil)

func (h *recentHistory) Len() int { return len(h.entries) }
func (h *recentHistory) At(i int) string {
	if i >= 0 && i < len(h.entries) {
		return h.entries[i]
	}
	return ""
}

// TUI is the terminal frontend. It drives an ollie-9p session via file I/O.
type TUI struct {
	sess      *session.Session
	appCancel context.CancelCauseFunc
	split     *splitInput
	history   recentHistory
	chatOff   int64 // current read offset in the chat file
}

// New creates a TUI backed by the given session.
func New(sess *session.Session) *TUI {
	return &TUI{sess: sess}
}

// Run starts the interactive readline loop, blocking until the user exits.
func (t *TUI) Run(ctx context.Context) {
	tt, err := gotty.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tty:", err)
		return
	}
	defer tt.Close()

	var ed multiline.Editor
	restorePaste, err := setupBracketedPaste(tt, &ed)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bracketed paste:", err)
	} else {
		defer restorePaste()
	}

	ed.SetPrompt(func(w io.Writer, lnum int) (int, error) {
		if lnum == 0 {
			return fmt.Fprint(w, t.sess.Prompt())
		}
		return fmt.Fprint(w, "... ")
	})

	ed.SubmitOnEnterWhen(func(lines []string, _ int) bool {
		if len(lines) <= 1 {
			return true
		}
		return !strings.HasSuffix(strings.TrimSpace(lines[len(lines)-1]), "\\")
	})

	ed.SetHistory(&t.history)
	ed.SetHistoryCycling(true)

	t.split = newSplitInput(tt, tt.Output(), t.sess.Prompt(), nil)
	t.split.onSubmit = func(input string) {
		if strings.HasPrefix(input, "/q ") {
			if prompt := strings.TrimSpace(input[3:]); prompt != "" {
				t.sess.Queue(prompt) //nolint:errcheck
			}
			return
		}
		// Mid-turn input is queued for after the current turn.
		t.sess.Queue(input) //nolint:errcheck
	}

	appCtx, appCancel := context.WithCancelCause(ctx)
	t.appCancel = appCancel
	defer appCancel(nil)

	// SIGTERM cancels the context; SIGINT interrupts a running turn.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-appCtx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				if sig == syscall.SIGTERM {
					appCancel(fmt.Errorf("signal: terminated"))
					return
				}
				if !t.sess.IsIdle() {
					t.sess.Interrupt() //nolint:errcheck
				}
			}
		}
	}()

	var lastCtrlC time.Time
	firstRead := true

	for appCtx.Err() == nil {
		if firstRead {
			firstRead = false
			if _, h, err := tt.Size(); err == nil && h > 0 {
				clearScreenAndMoveToBottom(tt.Output(), h)
			}
			if f, err := os.Open(t.sess.ChatPath()); err == nil {
				n, _ := io.Copy(os.Stdout, f)
				t.chatOff = n
				f.Close()
			}
		}

		lines, err := ed.Read(appCtx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			errs := err.Error()
			if errs == "interrupted" || errs == "^C" {
				now := time.Now()
				if !lastCtrlC.IsZero() && now.Sub(lastCtrlC) <= ctrlCExitWindow {
					break
				}
				lastCtrlC = now
				fmt.Fprint(os.Stderr, "^C (press Ctrl-C again to exit)\n")
				continue
			}
			fmt.Fprintln(os.Stderr, "input error:", err)
			break
		}

		input := strings.Join(lines, "\n")
		lastCtrlC = time.Time{}

		if strings.TrimSpace(input) == "" {
			continue
		}

		t.history.entries = append(t.history.entries, input)

		if len(lines) == 1 {
			if w, _, err := tt.Size(); err == nil {
				prompt := t.sess.Prompt()
				if shouldRerenderSubmittedSingleLine(prompt, input, w) {
					rerenderSubmittedSingleLine(tt.Output(), prompt, input)
				}
			}
		}

		t.processInputWithSplit(appCtx, input, &ed)
	}
}

func (t *TUI) processInputWithSplit(ctx context.Context, input string, ed *multiline.Editor) {
	if input == "/kill" {
		if err := t.sess.Kill(); err != nil {
			fmt.Fprintln(os.Stderr, "kill:", err)
		}
		t.appCancel(fmt.Errorf("session killed"))
		return
	}

	if cmd, ok, err := parseQueuedCommandLine(input); ok {
		var msg string
		if err != nil {
			msg = err.Error()
		} else if t.split == nil {
			_, msg = runQueuedCommand(nil, cmd)
		} else {
			msg = t.split.runQueuedCommand(cmd)
		}
		fmt.Fprintln(os.Stdout, msg)
		return
	}

	if strings.HasPrefix(input, "/q ") {
		prompt := strings.TrimSpace(input[3:])
		if prompt != "" {
			t.sess.Queue(prompt) //nolint:errcheck
			fmt.Fprintf(os.Stderr, "queued: %s\n", prompt)
		}
		return
	}

	if !t.sess.IsIdle() {
		t.sess.Queue(input) //nolint:errcheck
		fmt.Fprintf(os.Stderr, "queued: %s\n", input)
		return
	}

	if t.split == nil {
		t.submitAndWait(ctx, input, os.Stdout)
		return
	}

	t.split.SetPrompt(t.sess.Prompt())
	wrapper := t.split.Enter()
	out := io.Writer(os.Stdout)
	if wrapper != nil {
		out = wrapper
	}

	t.submitAndWait(ctx, input, out)

	_, pending := t.split.Exit()
	if pending != "" {
		ed.SetDefault([]string{pending})
	}
}

// submitAndWait writes the prompt to the session and tails the chat file,
// writing new bytes to out, until the agent returns to idle.
func (t *TUI) submitAndWait(ctx context.Context, input string, out io.Writer) {
	if err := t.sess.Submit(input); err != nil {
		fmt.Fprintln(os.Stderr, "submit:", err)
		return
	}

	f, err := os.Open(t.sess.ChatPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "open chat:", err)
		waitIdle(ctx, t.sess)
		return
	}
	defer f.Close()

	if _, err := f.Seek(t.chatOff, io.SeekStart); err != nil {
		fmt.Fprintln(os.Stderr, "seek chat:", err)
	}

	buf := make([]byte, 4096)
	// Brief grace period for the server to dispatch the write and update state.
	time.Sleep(150 * time.Millisecond)

	for ctx.Err() == nil {
		// Drain all available output.
		for {
			n, _ := f.Read(buf)
			if n == 0 {
				break
			}
			out.Write(buf[:n]) //nolint:errcheck
			t.chatOff += int64(n)
		}

		if t.sess.IsIdle() {
			// One final drain to catch output written just before state went idle.
			for {
				n, _ := f.Read(buf)
				if n == 0 {
					break
				}
				out.Write(buf[:n]) //nolint:errcheck
				t.chatOff += int64(n)
			}
			break
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// waitIdle polls state until the agent is idle or the context is done.
func waitIdle(ctx context.Context, sess *session.Session) {
	for ctx.Err() == nil {
		if sess.IsIdle() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func squashWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
