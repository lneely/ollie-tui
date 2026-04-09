# ollie-tui

Terminal UI frontend for [ollie](https://github.com/lneely/ollie). Provides the interactive readline loop, split input, and output rendering. The agent core, backends, and tools live in the ollie library.

## Build

```
mk
```

Installs the `ollie` binary to `~/bin`.

## Configuration

All configuration is shared with the ollie library. See the [ollie README](https://github.com/lneely/ollie) for full details on environment variables, backend selection, MCP servers, and hooks.

Quick reference:

```
~/.config/ollie/env       — default environment (OLLIE_BACKEND, OLLIE_MODEL, keys, etc.)
~/.config/ollie/config.json — MCP servers, hooks, generation params
~/.config/ollie/agents/   — named agent configs
```

## Usage

```
ollie [agent]
ollie --session <id>
ollie --prompt <text>
```

- `agent` — load a named config from `~/.config/ollie/agents/<agent>.json`
- `--session` — resume a saved session by ID
- `--prompt` — run a single prompt non-interactively and exit

## UI

- **Enter** — submit
- **Ctrl+J** — insert newline
- **Ctrl+C** — interrupt; press twice to exit

## License

GPLv3
