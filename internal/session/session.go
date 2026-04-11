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

// MountPath returns the ollie-9p mount point from OLLIE_9MOUNT, or ~/mnt/ollie.
func MountPath() string {
	if m := os.Getenv("OLLIE_9MOUNT"); m != "" {
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

// SaveLastSession writes the session ID to the last-session file.
func SaveLastSession(id string) error {
	path := lastSessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(id+"\n"), 0600)
}

// LoadLastSession reads the last-session file and returns the session ID,
// or an empty string if the file is missing or empty.
func LoadLastSession() (string, error) {
	data, err := os.ReadFile(lastSessionPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// Attach returns a Session for an existing session ID, verifying it exists.
func Attach(mount, id string) (*Session, error) {
	info, err := os.Stat(filepath.Join(mount, "s", id))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return &Session{Mount: mount, ID: id}, nil
}

// Create creates a new session by writing to the root ctl file and waiting
// for the corresponding directory to appear under s/. opts may contain
// backend, model, agent, and workdir values; empty values are omitted.
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
	cmd.WriteString("new")
	for _, k := range []string{"backend", "model", "agent", "workdir"} {
		if v := opts[k]; v != "" {
			cmd.WriteByte(' ')
			cmd.WriteString(k)
			cmd.WriteByte('=')
			cmd.WriteString(v)
		}
	}
	if err := os.WriteFile(filepath.Join(mount, "ctl"), []byte(cmd.String()+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("write ctl: %w", err)
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

// Kill destroys the session by writing to the root ctl file.
func (s *Session) Kill() error {
	return os.WriteFile(filepath.Join(s.Mount, "ctl"), []byte("kill "+s.ID+"\n"), 0644)
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

// Interrupt sends a stop signal to the session.
func (s *Session) Interrupt() error {
	return os.WriteFile(s.path("ctl"), []byte("stop\n"), 0644)
}

// IsIdle reports whether the agent is currently idle.
func (s *Session) IsIdle() bool {
	data, err := os.ReadFile(s.path("state"))
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) == "idle"
}
