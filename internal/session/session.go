// Package session provides a client for an ollie-9p session via plain file I/O.
package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Session represents an active ollie-9p session accessed through the mounted filesystem.
type Session struct {
	Mount string
	ID    string
}

// MountPath returns the ollie-9p mount point from OLLIE, or ~/mnt/ollie.
func MountPath() string {
	if m := os.Getenv("OLLIE"); m != "" {
		return m
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "mnt", "ollie")
}

func listSessionIDs(mount string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(mount, "s"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// lastSessionPath returns the path to the last-session file.
func lastSessionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ollie", "last-session")
}

// readLastSessionLines reads the last-session file and returns non-empty lines.
func readLastSessionLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// SaveLastSession upserts the {cwd}\t{id} entry for the given working directory.
func SaveLastSession(cwd, id string) error {
	path := lastSessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lines, _ := readLastSessionLines(path)
	found := false
	for i, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] == cwd {
			lines[i] = cwd + "\t" + id
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, cwd+"\t"+id)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

// LoadLastSession returns the session ID for the given cwd, or "" if not found.
func LoadLastSession(cwd string) (string, error) {
	lines, err := readLastSessionLines(lastSessionPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] == cwd {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	return "", nil
}

// updateLastSessionID replaces oldID with newID across all entries (used by Rename).
func updateLastSessionID(oldID, newID string) error {
	path := lastSessionPath()
	lines, err := readLastSessionLines(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for i, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == oldID {
			lines[i] = parts[0] + "\t" + newID
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

// Attach returns a Session for an existing session ID, verifying it exists.
func Attach(mount, id string) (*Session, error) {
	info, err := os.Stat(filepath.Join(mount, "s", id))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return &Session{Mount: mount, ID: id}, nil
}

// Create creates a new session by writing KV pairs to s/new and waiting
// for the corresponding directory to appear under s/. opts may contain
// backend, model, agent, and cwd values; empty values are omitted.
func Create(mount string, opts map[string]string) (*Session, error) {
	before, err := listSessionIDs(mount)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	beforeSet := make(map[string]bool, len(before))
	for _, id := range before {
		beforeSet[id] = true
	}

	var cmd strings.Builder
	for _, k := range []string{"backend", "model", "agent", "cwd"} {
		if v := opts[k]; v != "" {
			cmd.WriteString(k)
			cmd.WriteByte('=')
			cmd.WriteString(v)
			cmd.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(mount, "s", "new"), []byte(cmd.String()), 0644); err != nil {
		return nil, fmt.Errorf("write s/new: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		after, err := listSessionIDs(mount)
		if err == nil {
			for _, id := range after {
				if !beforeSet[id] {
					return &Session{Mount: mount, ID: id}, nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("timeout waiting for new session")
}

// Kill destroys the session by removing its directory.
func (s *Session) Kill() error {
	return os.Remove(filepath.Join(s.Mount, "s", s.ID))
}

func (s *Session) path(name string) string {
	return filepath.Join(s.Mount, "s", s.ID, name)
}

// ChatPath returns the path to the append-only chat log.
func (s *Session) ChatPath() string { return s.path("chat") }

// Prompt returns the readline prompt string.
func (s *Session) Prompt() string { return "> " }

// Submit writes a prompt to the agent. The server dispatches it asynchronously on close.
func (s *Session) Submit(text string) error {
	return os.WriteFile(s.path("prompt"), []byte(text), 0644)
}

// Queue enqueues a prompt for execution after the current turn.
func (s *Session) Queue(text string) error {
	return os.WriteFile(s.path("enqueue"), []byte(text), 0644)
}

// Stop sends a stop signal to the session.
func (s *Session) Stop() error {
	return os.WriteFile(s.path("ctl"), []byte("stop\n"), 0644)
}

// Control sends a slash command to the session ctl file (e.g. "/cwd /path").
func (s *Session) Control(cmd string) error {
	return os.WriteFile(s.path("ctl"), []byte(cmd+"\n"), 0644)
}

// IsIdle reports whether the agent is currently idle.
func (s *Session) IsIdle() bool {
	data, err := os.ReadFile(s.path("state"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "idle"
}

// SystemPrompt returns the fully rendered system prompt for this session.
func (s *Session) SystemPrompt() (string, error) {
	data, err := os.ReadFile(s.path("systemprompt"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// InjectEnv writes comma-separated var names to s/{id}/env, injecting their
// current process values into the session.
func (s *Session) InjectEnv(vars string) {
	var sb strings.Builder
	for _, name := range strings.Split(vars, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if v := os.Getenv(name); v != "" {
			fmt.Fprintf(&sb, "%s=%s\n", name, v)
		}
	}
	if sb.Len() > 0 {
		os.WriteFile(s.path("env"), []byte(sb.String()), 0600) //nolint:errcheck
	}
}

// Rename changes the session's directory name via os.Rename (wstat on 9P).
// Updates the in-memory ID and the last-session file on success.
func (s *Session) Rename(newName string) error {
	oldDir := filepath.Join(s.Mount, "s", s.ID)
	newDir := filepath.Join(s.Mount, "s", newName)
	if err := os.Rename(oldDir, newDir); err != nil {
		return err
	}
	updateLastSessionID(s.ID, newName) //nolint:errcheck
	s.ID = newName
	return nil
}
