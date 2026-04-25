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

func lastSessionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ollie", "last-session")
}

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

// Create creates a new session by writing KV pairs to s/new.
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
	for _, k := range []string{"name", "backend", "model", "agent", "cwd"} {
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

	if name := opts["name"]; name != "" {
		return &Session{Mount: mount, ID: name}, nil
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

// Submit writes a prompt to the session prompt file.
func (s *Session) Submit(text string) error {
	return os.WriteFile(s.path("prompt"), []byte(text), 0644)
}

// Stop sends a stop signal via ctl.
func (s *Session) Stop() error {
	return os.WriteFile(s.path("ctl"), []byte("stop\n"), 0644)
}

// CfgGet reads a key from the cfg file.
func (s *Session) CfgGet(key string) string {
	data, err := os.ReadFile(s.path("cfg"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && k == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// IsIdle reports whether the agent is currently idle.
func (s *Session) IsIdle() bool { return s.CfgGet("state") == "idle" }

// WaitStateChange blocks on the statewait file until the state changes.
// Returns the new state string.
func (s *Session) WaitStateChange() string {
	data, err := os.ReadFile(s.path("statewait"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ReadFile reads a session file (e.g. "models", "usage", "ctxsz").
func (s *Session) ReadFile(name string) (string, error) {
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Rename changes the session directory name.
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

// Control writes a command to the ctl file.
func (s *Session) Control(cmd string) error {
	return os.WriteFile(s.path("ctl"), []byte(cmd+"\n"), 0644)
}

// CfgWrite writes key=value pairs to the cfg file.
func (s *Session) CfgWrite(content string) error {
	return os.WriteFile(s.path("cfg"), []byte(content), 0644)
}

// Queue enqueues a prompt for later execution.
func (s *Session) Queue(text string) error {
	return os.WriteFile(s.path("fifo.in"), []byte(text), 0644)
}

// ChatSize returns the current size of the chat file.
func (s *Session) ChatSize() int64 {
	info, err := os.Stat(s.ChatPath())
	if err != nil {
		return 0
	}
	return info.Size()
}
