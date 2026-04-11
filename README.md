# ollie-tui

Terminal UI frontend for [ollie](https://github.com/lneely/ollie) via [ollie-9p](https://github.com/lneely/ollie-9p). Provides the interactive readline loop, split input, and output rendering. The agent core, backends, and tools are handled by the ollie-9p server.

## Background

ollie-tui started as a direct integration with the ollie library — a way to verify that the agent core worked end-to-end. It used ollie's Go API directly: constructing backends, dispatching tools, streaming typed events through a callback.

Once that proved out, the question became whether the same TUI could be built entirely against the [ollie-9p](https://github.com/lneely/ollie-9p) filesystem interface — with no knowledge of ollie's internals, no SDK, no event types. The answer is yes. The current implementation uses only the Go standard library for its ollie integration: plain `os.WriteFile` to submit prompts and `os.Read` in a poll loop to tail the chat log.

The point is not the TUI itself. It is that a real, interactive client — with readline, split input, mid-turn queueing, interrupt handling — can be built on top of a 9P filesystem with no more machinery than a shell script would use. The filesystem interface is not a simplified view of the API; it is sufficient on its own.

## Prerequisites

[olliesrv](https://github.com/lneely/ollie-9p) must be running and mounted before starting ollie-tui:

```sh
olliesrv start
```

By default the server mounts at `~/mnt/ollie`. Set `OLLIE_9MOUNT` to use a different path.

## Build

```
mk
```

Installs the `ollie` binary to `~/bin`.

## Usage

```
ollie [--mount <path>] [--backend <name>] [--model <name>] [--agent <name>] [--workdir <path>]
```

On startup, a new session is created and its ID is printed to stderr. On exit, the session is destroyed.

| Flag | Description |
|------|-------------|
| `--mount` | ollie-9p mount path (default: `$OLLIE_9MOUNT` or `~/mnt/ollie`) |
| `--backend` | backend for the new session (e.g. `ollama`) |
| `--model` | model for the new session (e.g. `qwen3:8b`) |
| `--agent` | agent config to load |
| `--workdir` | working directory for tool execution and system prompt |

## UI

- **Enter** — submit prompt
- **Ctrl+J** — insert newline
- **Ctrl+C** — interrupt running turn; press twice to exit
- `/q <prompt>` — queue a prompt for execution after the current turn

During a running turn, typed input is queued rather than injected.

## License

GPLv3
