package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"ollie/pkg/agent"
	"ollie/pkg/backend"
	"ollie/pkg/config"
	"ollie/pkg/tools"
	execute "ollie/pkg/tools/execute"
	"ollie/pkg/tools/file"
	"ollie-tui/internal/tui"
)

func main() {
	sessionFlag := flag.String("session", "", "resume a session by ID")
	promptFlag := flag.String("prompt", "", "run a single prompt non-interactively and exit")
	flag.Parse()
	extraArgs := flag.Args()

	agentsDir := agent.DefaultAgentsDir()
	sessionsDir := agent.DefaultSessionsDir()
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "sessions dir:", err)
		os.Exit(1)
	}

	be, err := backend.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create backend:", err)
		os.Exit(1)
	}

	if modelName := os.Getenv("OLLIE_MODEL"); modelName != "" {
		be.SetModel(modelName)
	}

	newDispatcher := tools.NewDispatcherFunc(map[string]func() tools.Server{
		"execute": execute.Decl(""),
		"file":    file.Decl,
	})

	agentName := os.Getenv("OLLIE_AGENT")
	if agentName == "" {
		agentName = "default"
	}

	sessionID := agent.NewSessionID()
	var resumeMessages []backend.Message
	if *sessionFlag != "" {
		sessionPath := sessionsDir + "/" + *sessionFlag + ".json"
		data, readErr := os.ReadFile(sessionPath)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, "--session:", readErr)
			os.Exit(1)
		}
		var ps agent.PersistedSession
		if jsonErr := json.Unmarshal(data, &ps); jsonErr != nil {
			fmt.Fprintln(os.Stderr, "--session: bad JSON:", jsonErr)
			os.Exit(1)
		}
		sessionID = ps.ID
		resumeMessages = ps.Messages
		if ps.Agent != "" && len(extraArgs) == 0 {
			agentName = ps.Agent
		}
	}
	if len(extraArgs) > 0 {
		agentName = extraArgs[0]
	}

	cfgPath := agent.AgentConfigPath(agentsDir, agentName)
	cfg, cfgErr := config.Load(cfgPath)
	env := agent.BuildAgentEnv(cfg, newDispatcher(), "")

	var initialSession *agent.Session
	if len(resumeMessages) > 0 {
		initialSession = agent.RestoreSession(resumeMessages)
	}

	agentCore := agent.NewAgentCore(agent.AgentCoreConfig{
		Backend:     be,
		AgentName:   agentName,
		AgentsDir:   agentsDir,
		SessionsDir: sessionsDir,
		SessionID:   sessionID,
		Session:     initialSession,
		Env:           env,
		NewDispatcher: newDispatcher,
	})

	if cfgErr != nil {
		fmt.Fprintln(os.Stderr, "agent config:", cfgErr)
	}
	for _, msg := range env.Messages {
		fmt.Fprintln(os.Stderr, msg)
	}
	if len(resumeMessages) > 0 {
		fmt.Fprintf(os.Stderr, "session: %s (resumed)\n", sessionID)
	} else {
		fmt.Fprintf(os.Stderr, "session: %s\n", sessionID)
	}

	env.Hooks.Run(context.Background(), agent.HookAgentSpawn, nil)

	if *promptFlag != "" {
		agentCore.Submit(context.Background(), *promptFlag, tui.MakeOutputFn(os.Stdout))
		return
	}

	tui.New(agentCore).Run(context.Background())
}


