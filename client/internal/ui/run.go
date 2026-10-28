package ui

import (
	"context"

	"github.com/thebanri/limoni"
	"github.com/thebanri/limoni/core/driver"
)

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
