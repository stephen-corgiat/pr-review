// Package eventlog is the Review Session's append-only JSONL event log
// (ADR 0002): one event per line, in the user data directory. Several
// processes append to the same log (the TUI, and the export and
// resolve), so every append is one write of whole lines, to a file opened
// with O_APPEND, under an exclusive flock; readers take a shared lock and
// only ever consume whole lines.
package eventlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// FileName is the log's name inside a Review Session's directory.
const FileName = "events.jsonl"

// Log is one Review Session's event log.
type Log struct {
	path string
	mu   sync.Mutex
	off  int64 // how far Load and Poll have read
}

// Open returns the log in dir (creating dir; the file is created on the
// first append).
func Open(dir string) (*Log, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Log{path: filepath.Join(dir, FileName)}, nil
}

// OpenExisting returns the log in dir without creating anything (for
// readers that must not leave traces, like shell completion).
func OpenExisting(dir string) *Log { return &Log{path: filepath.Join(dir, FileName)} }

// Path is the log file's path.
func (l *Log) Path() string { return l.path }

// Exists reports whether the log has any events yet.
func (l *Log) Exists() bool {
	fi, err := os.Stat(l.path)
	return err == nil && fi.Size() > 0
}

// Load reads every event from the start.
func (l *Log) Load() ([]session.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.off = 0
	return l.readNew()
}

// Poll reads the events appended since the last Load or Poll, by anyone
// (the engine skips its own).
func (l *Log) Poll() ([]session.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readNew()
}

func (l *Log) readNew() ([]session.Event, error) {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return nil, fmt.Errorf("locking %s: %w", l.path, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if _, err := f.Seek(l.off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// Only whole lines: a writer without the lock (there shouldn't be one)
	// could leave a partial last line.
	end := bytes.LastIndexByte(b, '\n')
	if end < 0 {
		return nil, nil
	}
	b = b[:end+1]
	var out []session.Event
	for _, raw := range bytes.SplitAfter(b, []byte("\n")) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		var ev session.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			return out, fmt.Errorf("%s: a bad line after offset %d: %w", l.path, l.off, err)
		}
		out = append(out, ev)
	}
	l.off += int64(len(b))
	return out, nil
}

// Append writes events at the end of the log in one write.
func (l *Log) Append(evs ...session.Event) error {
	if len(evs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, ev := range evs {
		b, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("encoding %s: %w", ev.Type(), err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking %s: %w", l.path, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if _, err := f.Write(buf.Bytes()); err != nil {
		return err
	}
	return f.Sync()
}
