// Package session starts a fancyband engine and talks to it. The engine
// runs as a child process with one end of a socket pair; the C engine and
// the Go client never share a process. That keeps Angband's GPL version 2
// code and Limoni's Apache 2.0 code in separate programs.
package session

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fancyband/internal/proto"
)

// ClientName is what the client calls itself in HELLO.
const ClientName = "fancyband"

// Options says how to start the engine and what to ask it for.
type Options struct {
	// Binary is the path to the fancyband program.
	Binary string
	// Args go to the engine before the net module's own options. They must
	// include -mnet and must not include "--".
	Args []string
	// ModuleArgs go to the net module after "--fd 3", for example
	// "--headless".
	ModuleArgs []string
	// Dir is the working directory. The engine finds its lib/ folder
	// relative to it. The default is the binary's folder, which suits a
	// development build; in a release, lib/ is one folder above bin/.
	Dir string
	// Env is the environment. The engine needs a UTF-8 locale, so when Env
	// is nil the session uses the current environment with LC_ALL set to
	// C.UTF-8 if no UTF-8 locale is set.
	Env []string
	// Stderr receives the engine's standard error. Nil discards it.
	Stderr io.Writer
	// Terms lists the terms to open. Nil opens the main term at 80 by 24.
	Terms []proto.TermSize
	// Caps lists the capabilities the client claims.
	Caps []string
}

// Session is one running engine.
type Session struct {
	// Server is the engine's HELLO.
	Server proto.Hello

	conn net.Conn
	r    *bufio.Reader
	cmd  *exec.Cmd

	sendMu sync.Mutex
}

// Start starts the engine and completes the handshake.
func Start(opts Options) (*Session, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("session: socketpair: %w", err)
	}
	ours := os.NewFile(uintptr(fds[0]), "fancyband-client")
	theirs := os.NewFile(uintptr(fds[1]), "fancyband-engine")
	defer theirs.Close()

	conn, err := net.FileConn(ours)
	ours.Close()
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}

	dir := opts.Dir
	if dir == "" {
		dir = filepath.Dir(opts.Binary)
	}
	// ExtraFiles[0] becomes file descriptor 3 in the child.
	args := append(append([]string{}, opts.Args...), "--", "--fd", "3")
	args = append(args, opts.ModuleArgs...)
	cmd := exec.Command(opts.Binary, args...)
	cmd.Dir = dir
	cmd.Env = opts.Env
	if cmd.Env == nil {
		cmd.Env = utf8Env(os.Environ())
	}
	cmd.Stderr = opts.Stderr
	cmd.ExtraFiles = []*os.File{theirs}
	if err := cmd.Start(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("session: starting %s: %w", opts.Binary, err)
	}

	s, err := handshake(conn, opts)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	s.cmd = cmd
	return s, nil
}

// Connect completes the handshake with an engine started separately with
// "-- --listen PATH".
func Connect(path string, opts Options) (*Session, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	return handshake(conn, opts)
}

func handshake(conn net.Conn, opts Options) (*Session, error) {
	s := &Session{conn: conn, r: bufio.NewReaderSize(conn, 64*1024)}
	terms := opts.Terms
	if terms == nil {
		terms = []proto.TermSize{{Index: 0, Width: 80, Height: 24}}
	}
	hello := proto.Hello{Version: proto.Version, Name: ClientName, Terms: terms, Caps: opts.Caps}
	if err := s.Send(hello); err != nil {
		conn.Close()
		return nil, fmt.Errorf("session: sending HELLO: %w", err)
	}
	m, err := s.Recv()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("session: waiting for HELLO: %w", err)
	}
	server, ok := m.(proto.Hello)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("session: expected HELLO, got %T", m)
	}
	s.Server = server
	return s, nil
}

// Send sends one message. It is safe to call from several goroutines.
func (s *Session) Send(m proto.Message) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return proto.WriteFrame(s.conn, m.Frame())
}

// Recv returns the next message. Only one goroutine may call it.
func (s *Session) Recv() (proto.Message, error) {
	f, err := proto.ReadFrame(s.r)
	if err != nil {
		return nil, err
	}
	return proto.Decode(f)
}

// Batch is a run of messages read together. Err is set on the last batch,
// when reading stopped; it is io.EOF if the engine closed the connection.
type Batch struct {
	Msgs []proto.Message
	Err  error
}

// maxBatch caps a batch so a busy engine cannot starve the UI.
const maxBatch = 4096

// Stream reads messages until the connection ends and sends them to out
// in batches, then closes out. A batch ends when no more bytes are
// waiting, so a burst of drawing arrives as one batch. Stream must be
// the only reader; run it on its own goroutine.
func (s *Session) Stream(out chan<- Batch) {
	defer close(out)
	var msgs []proto.Message
	for {
		m, err := s.Recv()
		if err != nil {
			out <- Batch{Msgs: msgs, Err: err}
			return
		}
		msgs = append(msgs, m)
		if s.r.Buffered() == 0 || len(msgs) >= maxBatch {
			out <- Batch{Msgs: msgs}
			msgs = nil
		}
	}
}

// SetReadDeadline makes Recv fail after t. The zero time removes the
// deadline.
func (s *Session) SetReadDeadline(t time.Time) error {
	return s.conn.SetReadDeadline(t)
}

// Close drops the connection. The engine saves and exits when it sees
// the connection close.
func (s *Session) Close() error {
	return s.conn.Close()
}

// Wait waits for the engine to exit, after closing the connection or
// receiving BYE. It returns nil for a session made with Connect.
func (s *Session) Wait() error {
	if s.cmd == nil {
		return nil
	}
	return s.cmd.Wait()
}

// Kill stops the engine at once, without saving.
func (s *Session) Kill() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("session: no engine process")
	}
	return s.cmd.Process.Kill()
}

// Signal sends the engine a signal.
func (s *Session) Signal(sig os.Signal) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("session: no engine process")
	}
	return s.cmd.Process.Signal(sig)
}

// utf8Env adds LC_ALL=C.UTF-8 unless the environment already picks a
// UTF-8 locale. The engine refuses to run without one.
func utf8Env(env []string) []string {
	// The first locale variable that is set decides, as in setlocale(3).
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			if k != name || v == "" {
				continue
			}
			v = strings.ToLower(v)
			if strings.Contains(v, "utf-8") || strings.Contains(v, "utf8") {
				return env
			}
			return append(env, "LC_ALL=C.UTF-8")
		}
	}
	return append(env, "LC_ALL=C.UTF-8")
}
