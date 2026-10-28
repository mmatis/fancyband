package ui

import (
	"context"
	"os"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/driver"
	"github.com/thebanri/limoni/graphics"
)

// tilesBlocked returns why the terminal cannot show tiles, or "" if it
// can. Limoni settles the image protocol when it starts, by asking the
// terminal, so this must run after limoni.New.
func tilesBlocked(proto graphics.Protocol) string {
	forced := os.Getenv("LIMONI_GRAPHICS") != ""
	switch proto {
	case graphics.ProtocolKitty:
		// tmux drops kitty pictures, but the kitty environment variables
		// pass through it, so Limoni would pick kitty anyway.
		if os.Getenv("TMUX") != "" && !forced {
			return "kitty pictures do not pass through tmux; set LIMONI_GRAPHICS to try anyway"
		}
		return ""
	case graphics.ProtocolSixel, graphics.ProtocolIterm2:
		return ""
	}
	return "the terminal has no image protocol Limoni can use"
}

// Run takes over the terminal, plays until the engine says BYE or the
// connection ends, and restores the terminal. The returned model reports
// how the session ended. cfg.Width and cfg.Height are filled in from the
// terminal.
func Run(ctx context.Context, cfg Config) (*Model, error) {
	term, err := limoni.New()
	if err != nil {
		return nil, err
	}
	defer term.Close()
	term.SetTitle("Fancyband")

	area := term.FrontBuffer().Area
	cfg.Width, cfg.Height = int(area.Width), int(area.Height)
	if cfg.Tiles > 0 {
		if why := tilesBlocked(term.Capabilities().GraphicsProto); why != "" {
			if cfg.Log != nil {
				cfg.Log.Printf("tiles off: %s", why)
			}
			cfg.Tiles = 0
		} else if cfg.Log != nil {
			cfg.Log.Printf("tiles on, image protocol %d", term.Capabilities().GraphicsProto)
		}
	}
	m := New(cfg)

	program := limoni.NewProgram(m,
		limoni.WithAltScreen(),
		// Ctrl+C reaches the model, which turns it into QUIT.
		limoni.WithProgramCatchCtrlC(true),
		limoni.WithoutProgramQuitKeys(),
		limoni.WithProgramMouseMode(driver.MouseClicks),
	)
	err = program.RunTerminal(ctx, term, term.Driver())
	return m, err
}
