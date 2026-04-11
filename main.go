package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"ollie-tui/internal/session"
	"ollie-tui/internal/tui"
)

func main() {
	mountFlag   := flag.String("mount", "", "ollie-9p mount path (default: $OLLIE_9MOUNT or ~/mnt/ollie)")
	sessionFlag := flag.String("session", "", "attach to an existing session by ID")
	resumeFlag  := flag.Bool("resume", false, "attach to the last session")
	newFlag     := flag.Bool("new", false, "force creation of a new session")
	backendFlag := flag.String("backend", "", "backend for new session")
	modelFlag   := flag.String("model", "", "model for new session")
	agentFlag   := flag.String("agent", "", "agent for new session")
	workdirFlag := flag.String("workdir", "", "working directory for new session (default: $PWD)")
	cwdFlag     := flag.String("cwd", "", "override working directory on startup (default: $PWD for new sessions)")
	flag.Parse()

	cwdExplicit := *cwdFlag != ""
	cwd := *cwdFlag
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	mount := *mountFlag
	if mount == "" {
		mount = session.MountPath()
	}

	var (
		sess    *session.Session
		err     error
		created bool
	)

	switch {
	case *resumeFlag:
		id, loadErr := session.LoadLastSession()
		if loadErr != nil || id == "" {
			fmt.Fprintln(os.Stderr, "no last session")
			os.Exit(1)
		}
		sess, err = session.Attach(mount, id)
		if err != nil {
			fmt.Fprintln(os.Stderr, "last session no longer running")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "session: %s (resumed)\n", sess.ID)

	case *sessionFlag != "":
		sess, err = session.Attach(mount, *sessionFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "attach session:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "session: %s (resumed)\n", sess.ID)

	case *newFlag:
		sess, err = session.Create(mount, sessionOpts(*backendFlag, *modelFlag, *agentFlag, *workdirFlag, cwd))
		if err != nil {
			fmt.Fprintln(os.Stderr, "create session:", err)
			os.Exit(1)
		}
		created = true
		fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)

	default:
		id, loadErr := session.LoadLastSession()
		if loadErr != nil || id == "" {
			sess, err = session.Create(mount, sessionOpts(*backendFlag, *modelFlag, *agentFlag, *workdirFlag, cwd))
			if err != nil {
				fmt.Fprintln(os.Stderr, "create session:", err)
				os.Exit(1)
			}
			created = true
			fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)
		} else {
			sess, err = session.Attach(mount, id)
			if err != nil {
				sess, err = session.Create(mount, sessionOpts(*backendFlag, *modelFlag, *agentFlag, *workdirFlag, cwd))
				if err != nil {
					fmt.Fprintln(os.Stderr, "create session:", err)
					os.Exit(1)
				}
				created = true
				fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)
			} else {
				fmt.Fprintf(os.Stderr, "session: %s (resumed)\n", sess.ID)
			}
		}
	}

	// For resumed/attached sessions, apply -cwd only when explicitly given.
	// New sessions already have the workdir set via sessionOpts.
	if cwdExplicit && !created {
		if err := sess.Control("/cwd " + cwd); err != nil {
			fmt.Fprintln(os.Stderr, "set workdir:", err)
		}
	}

	session.SaveLastSession(sess.ID) //nolint:errcheck

	tui.New(sess).Run(context.Background())
}

// sessionOpts builds the opts map for session.Create. workdir defaults to cwd
// (the TUI's $PWD) if not explicitly given via -workdir.
func sessionOpts(backend, model, agent, workdir, cwd string) map[string]string {
	if workdir == "" {
		workdir = cwd
	}
	return map[string]string{
		"backend": backend,
		"model":   model,
		"agent":   agent,
		"workdir": workdir,
	}
}
