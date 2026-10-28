//go:build limoni_debug

package limoni

import "github.com/thebanri/limoni/automation"

type automationGateway struct {
	server   *automation.Server
	injected chan Event
}

func openGateway(cfg appConfig) (gateway, error) {
	if cfg.automationPath == "" {
		return nil, nil
	}
	server, err := automation.Listen(cfg.automationPath, automation.WithPolicy(automation.Policy(cfg.automationPolicy)))
	if err != nil {
		return nil, err
	}
	return &automationGateway{server: server, injected: make(chan Event, 64)}, nil
}

func (g *automationGateway) publish(f *Frame) {
	focused := ""
	if f.FocusManager != nil {
		focused = f.FocusManager.Focused()
	}
	g.server.Publish(automation.Snapshot{
		Tree:    f.AccessibilityTree(),
		Screen:  f.Buffer.Snapshot(),
		Focused: focused,
		Width:   f.Buffer.Area.Width,
		Height:  f.Buffer.Area.Height,
		Injector: func(ev Event) {
			select {
			case g.injected <- ev:
			default:
				// Dropped rather than blocking the automation handler: a client
				// that outruns the render loop gets backpressure as a lost event,
				// not a deadlocked application.
			}
		},
	})
}

func (g *automationGateway) events() <-chan Event { return g.injected }

func (g *automationGateway) close() error { return g.server.Close() }
