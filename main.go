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
	workdirFlag := flag.String("workdir", "", "working directory for new session")
	flag.Parse()

	mount := *mountFlag
	if mount == "" {
		mount = session.MountPath()
	}

	var (
		sess *session.Session
		err  error
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
		opts := map[string]string{
			"backend": *backendFlag,
			"model":   *modelFlag,
			"agent":   *agentFlag,
			"workdir": *workdirFlag,
		}
		sess, err = session.Create(mount, opts)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create session:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)

	default:
		// Try to resume last session by default
		id, loadErr := session.LoadLastSession()
		if loadErr != nil || id == "" {
			// No last session, create a new one
			opts := map[string]string{
				"backend": *backendFlag,
				"model":   *modelFlag,
				"agent":   *agentFlag,
				"workdir": *workdirFlag,
			}
			sess, err = session.Create(mount, opts)
			if err != nil {
				fmt.Fprintln(os.Stderr, "create session:", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)
		} else {
			sess, err = session.Attach(mount, id)
			if err != nil {
				// Last session no longer exists, create a new one
				opts := map[string]string{
					"backend": *backendFlag,
					"model":   *modelFlag,
					"agent":   *agentFlag,
					"workdir": *workdirFlag,
				}
				sess, err = session.Create(mount, opts)
				if err != nil {
					fmt.Fprintln(os.Stderr, "create session:", err)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)
			} else {
				fmt.Fprintf(os.Stderr, "session: %s (resumed)\n", sess.ID)
			}
		}
	}

	// Track the most recently used session for --resume.
	session.SaveLastSession(sess.ID) //nolint:errcheck

	tui.New(sess).Run(context.Background())
}