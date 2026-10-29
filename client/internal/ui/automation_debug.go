//go:build limoni_debug

package ui

import (
	"context"
	"time"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/automation"
)

// automated serves each frame the model draws on Limoni's automation
// socket, so tests and agents such as limoni-mcp can read the screen and
// send keys. Limoni does this itself only for limoni.Run, not for a
// Program, so the client does it here.
type automated struct {
	*Model
	server  *automation.Server
	program *limoni.Program
}

// withAutomation wraps m to serve the socket at path. Call attach with
// the program before it runs, and stop when it ends.
func withAutomation(m *Model, path string) (model limoni.Model, attach func(*limoni.Program), stop func(), err error) {
	server, err := automation.Listen(path, automation.WithPolicy(automation.Policy{
		ExposeInputValues: true,
		ExposeScreen:      true,
		AllowInput:        true,
	}))
	if err != nil {
		return nil, nil, nil, err
	}
	a := &automated{Model: m, server: server}
	return a, func(p *limoni.Program) { a.program = p }, func() { server.Close() }, nil
}

func (a *automated) View(f *limoni.Frame) {
	a.Model.View(f)
	focused := ""
	if f.FocusManager != nil {
		focused = f.FocusManager.Focused()
	}
	a.server.Publish(automation.Snapshot{
		Tree:    f.AccessibilityTree(),
		Screen:  f.Buffer.Snapshot(),
		Focused: focused,
		Width:   f.Buffer.Area.Width,
		Height:  f.Buffer.Area.Height,
		Injector: func(ev limoni.Event) {
			// A full queue drops the event rather than stalling the socket.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			a.program.SendDriver(ctx, ev)
		},
	})
}
