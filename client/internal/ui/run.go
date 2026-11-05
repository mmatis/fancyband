package ui

import (
	"context"
	"os"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/graphics"

	"fancyband/internal/settings"
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

// protocolName names an image protocol for the options screen.
func protocolName(p graphics.Protocol) string {
	switch p {
	case graphics.ProtocolKitty:
		return "Kitty"
	case graphics.ProtocolSixel:
		return "Sixel"
	case graphics.ProtocolIterm2:
		return "iTerm2"
	}
	return "none"
}

// konsoleGraphics returns "kitty" when the client runs in Konsole and
// nothing has chosen an image protocol yet. Konsole shows Kitty's pictures
// and Sixel; only Kitty's can slide and scroll to the pixel, and Limoni
// cannot see Konsole from its environment. Inside tmux the variable
// passes through but the pictures do not.
func konsoleGraphics(getenv func(string) string) string {
	if getenv("LIMONI_GRAPHICS") == "" && getenv("KONSOLE_VERSION") != "" && getenv("TMUX") == "" {
		return "kitty"
	}
	return ""
}

// Run takes over the terminal, plays until the engine says BYE or the
// connection ends, and restores the terminal. The returned model reports
// how the session ended. cfg.Width and cfg.Height are filled in from the
// terminal.
func Run(ctx context.Context, cfg Config) (*Model, error) {
	if cfg.SettingsPath != "" {
		s, err := settings.Load(cfg.SettingsPath)
		if err != nil {
			cfg.settingsErr = err
		} else {
			cfg.applySettings(s)
		}
	}
	// Tiles can be turned on later, so pick Konsole's protocol anyway.
	if p := konsoleGraphics(os.Getenv); p != "" {
		os.Setenv("LIMONI_GRAPHICS", p)
	}
	term, err := limoni.New()
	if err != nil {
		return nil, err
	}
	defer term.Close()
	term.SetTitle("Fancyband")

	area := term.FrontBuffer().Area
	cfg.Width, cfg.Height = int(area.Width), int(area.Height)
	gp := term.Capabilities().GraphicsProto
	cfg.Protocol = protocolName(gp)
	if why := tilesBlocked(gp); why != "" {
		cfg.TilesBlocked = why
		if cfg.Tiles > 0 && cfg.Log != nil {
			cfg.Log.Printf("tiles off: %s", why)
		}
		cfg.Tiles = 0
	} else if cfg.Tiles > 0 && cfg.Log != nil {
		cfg.Log.Printf("tiles on, image protocol %s", cfg.Protocol)
	}
	cfg.fullRedraw = term.ForceFullRedraw
	cfg.setMouseMode = term.SetMouseMode
	switch gp {
	case graphics.ProtocolKitty, graphics.ProtocolSixel, graphics.ProtocolIterm2:
		cfg.cellPixels = term.Driver().CellPixelSize
	}
	m := New(cfg)

	var model limoni.Model = m
	attach := func(*limoni.Program) {}
	if cfg.Automation != "" {
		var stop func()
		model, attach, stop, err = withAutomation(m, cfg.Automation)
		if err != nil {
			return nil, err
		}
		defer stop()
	}

	program := limoni.NewProgram(model,
		limoni.WithAltScreen(),
		// Ctrl+C reaches the model, which turns it into QUIT.
		limoni.WithProgramCatchCtrlC(true),
		limoni.WithoutProgramQuitKeys(),
		// Every move of the mouse only with hover on; see hover.go.
		limoni.WithProgramMouseMode(m.wantMouseMode()),
	)
	attach(program)
	err = program.RunTerminal(ctx, term, term.Driver())
	return m, err
}
