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

type recentHistory struct{ entries []string }

var _ readline.IHistory = (*recentHistory)(nil)

func (h *recentHistory) Len() int        { return len(h.entries) }
func (h *recentHistory) At(i int) string {
	if i >= 0 && i < len(h.entries) {
		return h.entries[i]
	}
	return ""
}

// TUI is the terminal frontend.
type TUI struct {
	sess      *session.Session
	appCancel context.CancelCauseFunc
	split     *splitInput
	history   recentHistory
	chatOff   int64
}

func New(sess *session.Session) *TUI {
	return &TUI{sess: sess}
}

// Run starts the interactive loop, blocking until the user exits.
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
		t.sess.Submit(input) //nolint:errcheck
	}

	appCtx, appCancel := context.WithCancelCause(ctx)
	t.appCancel = appCancel
	defer appCancel(nil)

	// SIGINT: stop running turn. SIGTERM: exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-appCtx.Done():
				return
			case sig := <-sigCh:
				if sig == syscall.SIGTERM {
					appCancel(fmt.Errorf("signal: terminated"))
					return
				}
				if !t.sess.IsIdle() {
					t.sess.Stop() //nolint:errcheck
				}
			}
		}
	}()

	// Show existing chat on attach.
	if _, h, err := tt.Size(); err == nil && h > 0 {
		clearScreenAndMoveToBottom(tt.Output(), h)
	}
	t.drainChat(os.Stdout)

	// If session is already running, tail until idle.
	if !t.sess.IsIdle() {
		t.tailUntilIdle(appCtx, os.Stdout)
	}

	var lastCtrlC time.Time

	for appCtx.Err() == nil {
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

		t.processInput(appCtx, input, &ed)
	}
}

func (t *TUI) processInput(ctx context.Context, input string, ed *multiline.Editor) {
	// Frontend-local commands.
	if strings.HasPrefix(input, `\`) {
		t.handleFrontendCmd(input[1:])
		return
	}

	// Slash commands handled client-side.
	if t.handleSlashCommand(input) {
		return
	}

	// Queued command management.
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

	// If agent is busy, just submit (inject/queue).
	if !t.sess.IsIdle() {
		t.sess.Submit(input) //nolint:errcheck
		return
	}

	// Regular prompt → submit with split input.
	if t.split == nil {
		t.submitAndTail(ctx, input, os.Stdout)
		return
	}

	t.split.SetPrompt(t.sess.Prompt())
	wrapper := t.split.Enter()
	out := io.Writer(os.Stdout)
	if wrapper != nil {
		out = wrapper
	}

	t.submitAndTail(ctx, input, out)

	_, pending := t.split.Exit()
	if pending != "" {
		ed.SetDefault([]string{pending})
	}
}

// submitAndTail writes the prompt, waits for the agent to start, then tails
// chat until idle. Follows the sh script protocol exactly:
//   echo prompt > prompt; cat statewait >/dev/null; while !idle: tail chat
func (t *TUI) submitAndTail(ctx context.Context, input string, out io.Writer) {
	if err := t.sess.Submit(input); err != nil {
		fmt.Fprintln(os.Stderr, "submit:", err)
		return
	}

	// Block until state leaves idle.
	t.sess.WaitStateChange()

	t.tailUntilIdle(ctx, out)
}

// handleSlashCommand handles commands client-side by reading session files
// directly. Returns true if handled.
func (t *TUI) handleSlashCommand(input string) bool {
	cmd, arg, _ := strings.Cut(input, " ")
	arg = strings.TrimSpace(arg)

	switch cmd {
	case "/stop":
		t.sess.Stop() //nolint:errcheck
	case "/kill":
		if err := t.sess.Kill(); err != nil {
			fmt.Fprintln(os.Stderr, "kill:", err)
			return true
		}
		t.appCancel(fmt.Errorf("session killed"))
	case "/compact":
		t.sess.Control("compact") //nolint:errcheck
	case "/clear":
		t.sess.Control("clear") //nolint:errcheck
	case "/models":
		t.catSessionFile("models")
	case "/usage":
		t.catSessionFile("usage")
	case "/ctxsz":
		t.catSessionFile("ctxsz")
	case "/sp":
		t.catSessionFile("systemprompt")
	case "/cwd":
		if arg == "" {
			fmt.Println(t.sess.CfgGet("cwd"))
		} else {
			t.sess.CfgWrite(fmt.Sprintf("cwd=%s\n", arg)) //nolint:errcheck
		}
	case "/agent":
		if arg == "" {
			fmt.Println(t.sess.CfgGet("agent"))
		} else {
			t.sess.CfgWrite(fmt.Sprintf("agent=%s\n", arg)) //nolint:errcheck
		}
	case "/model":
		if arg == "" {
			fmt.Println(t.sess.CfgGet("model"))
		} else {
			t.sess.CfgWrite(fmt.Sprintf("model=%s\n", arg)) //nolint:errcheck
		}
	case "/backend":
		if arg == "" {
			fmt.Println(t.sess.CfgGet("backend"))
		} else {
			t.sess.CfgWrite(fmt.Sprintf("backend=%s\n", arg)) //nolint:errcheck
		}
	case "/backends":
		t.catMountFile("backends")
	case "/tools":
		t.catMountDir("t")
	case "/skills":
		t.catMountDir("sk")
	case "/agents":
		t.catMountDir("a")
	case "/rn":
		if arg == "" {
			fmt.Fprintln(os.Stderr, "usage: /rn <new-name>")
		} else if err := t.sess.Rename(arg); err != nil {
			fmt.Fprintln(os.Stderr, "rename:", err)
		} else {
			cwd, _ := os.Getwd()
			session.SaveLastSession(cwd, arg) //nolint:errcheck
			fmt.Fprintf(os.Stderr, "renamed session to: %s\n", arg)
		}
	case "/q":
		if arg == "" {
			fmt.Fprintln(os.Stderr, "usage: /q <prompt>")
		} else {
			t.sess.Queue(arg) //nolint:errcheck
			fmt.Fprintln(os.Stderr, "queued")
		}
	default:
		return false
	}
	return true
}

func (t *TUI) catSessionFile(name string) {
	data, err := t.sess.ReadFile(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return
	}
	fmt.Print(data)
}

func (t *TUI) catMountFile(name string) {
	data, err := os.ReadFile(t.sess.Mount + "/" + name)
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return
	}
	fmt.Print(string(data))
}

func (t *TUI) catMountDir(subdir string) {
	entries, err := os.ReadDir(t.sess.Mount + "/" + subdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, subdir+":", err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			fmt.Println("  " + e.Name())
		}
	}
}

// drainChat reads all new bytes from the chat file.
func (t *TUI) drainChat(out io.Writer) {
	f, err := os.Open(t.sess.ChatPath())
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.chatOff, io.SeekStart); err != nil {
		return
	}
	cw := newDiffColorWriter(out)
	buf := make([]byte, 4096)
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			break
		}
		cw.Write(buf[:n]) //nolint:errcheck
		t.chatOff += int64(n)
	}
	cw.Flush()
}

// tailUntilIdle tails the chat file until the session returns to idle.
// Same protocol as the sh script:
//   while not idle: read new chat bytes, sleep 50ms
//   final drain
func (t *TUI) tailUntilIdle(ctx context.Context, out io.Writer) {
	f, err := os.Open(t.sess.ChatPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "open chat:", err)
		return
	}
	defer f.Close()

	if _, err := f.Seek(t.chatOff, io.SeekStart); err != nil {
		fmt.Fprintln(os.Stderr, "seek chat:", err)
	}

	buf := make([]byte, 4096)
	cw := newDiffColorWriter(out)

	for ctx.Err() == nil {
		if t.sess.IsIdle() {
			break
		}
		for {
			n, _ := f.Read(buf)
			if n == 0 {
				break
			}
			cw.Write(buf[:n]) //nolint:errcheck
			t.chatOff += int64(n)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Final drain.
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			break
		}
		cw.Write(buf[:n]) //nolint:errcheck
		t.chatOff += int64(n)
	}
	cw.Flush()
}

func (t *TUI) handleFrontendCmd(cmd string) {
	name, arg, _ := strings.Cut(cmd, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "switch":
		if arg == "" {
			fmt.Fprintln(os.Stderr, `usage: \switch <session-id>`)
			return
		}
		newSess, err := session.Attach(t.sess.Mount, arg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "switch:", err)
			return
		}
		t.sess = newSess
		t.chatOff = 0
		cwd, _ := os.Getwd()
		session.SaveLastSession(cwd, arg) //nolint:errcheck
		fmt.Fprintf(os.Stderr, "switched to session: %s\n", arg)
	default:
		fmt.Fprintln(os.Stderr, `\switch <id>  switch session`)
	}
}
