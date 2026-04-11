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
	backendFlag := flag.String("backend", "", "backend for new session")
	modelFlag   := flag.String("model", "", "model for new session")
	agentFlag   := flag.String("agent", "", "agent for new session")
	workdirFlag := flag.String("workdir", "", "working directory for new session")
	flag.Parse()

	mount := *mountFlag
	if mount == "" {
		mount = session.MountPath()
	}

	opts := map[string]string{
		"backend": *backendFlag,
		"model":   *modelFlag,
		"agent":   *agentFlag,
		"workdir": *workdirFlag,
	}

	sess, err := session.Create(mount, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create session:", err)
		os.Exit(1)
	}
	defer sess.Kill() //nolint:errcheck

	fmt.Fprintf(os.Stderr, "session: %s\n", sess.ID)

	tui.New(sess).Run(context.Background())
}
