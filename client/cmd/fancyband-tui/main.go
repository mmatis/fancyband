// Command fancyband-tui plays Fancyband in a terminal. It starts the
// fancyband engine as a separate program, draws the screens the engine
// streams, and sends it your keys.
//
// Usage:
//
//	fancyband-tui [flags] [-- engine options]
//
// Options after "--" go to the engine, for example "-- -n" for a new
// character or "-- -uName" to pick a save. Ctrl+C saves and quits, as does
// the game's own Ctrl+X.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"fancyband/internal/proto"
	"fancyband/internal/session"
	"fancyband/internal/settings"
	"fancyband/internal/tiles"
	"fancyband/internal/ui"
)

// version is the client version. Release builds override it with
// -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	enginePath := flag.String("engine", "", "path to the fancyband program (default: $FANCYBAND_BIN, next to this program, on $PATH, or build/engine/game in this checkout)")
	subwindows := flag.Bool("subwindows", true, "show the message log and monster list when the window has room")
	logDir := flag.String("log-dir", "", "folder for fancyband-tui.log and fancyband-engine.log (default ~/.angband/Fancyband)")
	tileSet := flag.Int("tiles", 0, "draw the map with tile set N from lib/tiles/list.txt: 1 Original, 2 Adam Bolt, 3 Gervais, 4 Nomad, 5 and 6 Shockbolt; 0 for text (default: as set in the options)")
	animate := flag.Bool("animate", true, "animate the tiles given animated filters (default: as set in the options)")
	imageProto := flag.String("graphics", "", "image protocol for tiles: kitty, sixel or iterm2 (default: ask the terminal)")
	automation := flag.String("automation", "", "serve the screen on this Unix socket for tests and agents such as limoni-mcp (needs a build with -tags limoni_debug)")
	dev := flag.Bool("dev", false, "developer mode: the start menu adds the Tile Editor (Defaults), which saves the default tiles in the source tree's engine/lib/tiles/fancyband")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: fancyband-tui [flags] [-- engine options]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("fancyband-tui", version)
		return
	}
	if *imageProto != "" {
		// Limoni reads this when it starts.
		os.Setenv("LIMONI_GRAPHICS", *imageProto)
	}
	// Flags given on the command line win over the settings file.
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })
	cfg := ui.Config{
		Subwindows:      *subwindows,
		SubwindowsGiven: given["subwindows"],
		Tiles:           *tileSet,
		TilesGiven:      given["tiles"],
		Animate:         *animate,
		AnimateGiven:    given["animate"],
		Automation:      *automation,
	}
	if err := run(*enginePath, *logDir, *dev, cfg, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "fancyband-tui:", err)
		os.Exit(1)
	}
}

func run(enginePath, logDir string, dev bool, cfg ui.Config, engineArgs []string) error {
	bin, err := findEngine(enginePath)
	if err != nil {
		return err
	}
	if dev {
		if cfg.DefaultsDir, err = findDefaults(bin); err != nil {
			return err
		}
		// Without -dev the engine reads the defaults from its own copy of
		// lib/, so keep that copy current, from the start and on each save.
		cfg.DefaultsCopy = filepath.Join(filepath.Dir(bin), "lib", "tiles", "fancyband")
		if err := tiles.MirrorDir(cfg.DefaultsDir, cfg.DefaultsCopy); err != nil {
			return fmt.Errorf("cannot update the engine's copy of the default tiles: %w", err)
		}
	}
	if logDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		logDir = filepath.Join(home, ".angband", "Fancyband")
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	clientLog, err := os.Create(filepath.Join(logDir, "fancyband-tui.log"))
	if err != nil {
		return err
	}
	defer clientLog.Close()
	engineLog, err := os.Create(filepath.Join(logDir, "fancyband-engine.log"))
	if err != nil {
		return err
	}
	defer engineLog.Close()

	logger := log.New(clientLog, "", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("fancyband-tui %s starting %s %q", version, bin, engineArgs)
	if cfg.DefaultsDir != "" {
		logger.Printf("developer mode: the default tiles are in %s", cfg.DefaultsDir)
	}

	// The client starts a new engine to switch characters; the model
	// waits for each old one, and this function for the last.
	var sess *session.Session
	start := func(args, module []string, terms []proto.TermSize, caps []string) (*session.Session, error) {
		s, err := session.Start(session.Options{
			Binary:     bin,
			Args:       append([]string{"-mnet"}, args...),
			ModuleArgs: module,
			Stderr:     engineLog,
			Terms:      terms,
			Caps:       caps,
		})
		if err == nil {
			sess = s
		}
		return s, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg.Start = start
	cfg.Log = logger
	cfg.EngineArgs = engineArgs
	cfg.SettingsPath = filepath.Join(home, ".angband", "Fancyband", settings.FileName)
	m, runErr := ui.Run(context.Background(), cfg)

	// The terminal is restored now. Let the engine finish saving; closing
	// the connection makes it save if it has not already.
	if sess != nil {
		sess.Close()
		waitEngine(sess, logger)
	}

	switch {
	case runErr != nil:
		logger.Print(runErr)
		return runErr
	case m.Err() != nil:
		return fmt.Errorf("%w (see %s)", m.Err(), engineLog.Name())
	case m.ByeReason() != "" && m.ByeReason() != "exit" && m.ByeReason() != "quit":
		return fmt.Errorf("the engine stopped: %s", m.ByeReason())
	}
	return nil
}

// waitEngine waits for the engine to exit, and stops it if it hangs.
func waitEngine(s *session.Session, logger *log.Logger) {
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		logger.Printf("engine exited: %v", err)
	case <-time.After(15 * time.Second):
		logger.Print("engine did not exit; stopping it")
		s.Kill()
		<-done
	}
}

// findEngine looks for the fancyband program.
func findEngine(flagPath string) (string, error) {
	if flagPath != "" {
		return checkEngine(flagPath)
	}
	if env := os.Getenv("FANCYBAND_BIN"); env != "" {
		return checkEngine(env)
	}
	if exe, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(exe), "fancyband")
		if _, err := os.Stat(beside); err == nil {
			return checkEngine(beside)
		}
	}
	if p, err := exec.LookPath("fancyband"); err == nil {
		return checkEngine(p)
	}
	// In a checkout, scripts/check builds the engine under build/.
	if dir, err := os.Getwd(); err == nil {
		for ; ; dir = filepath.Dir(dir) {
			built := filepath.Join(dir, "build", "engine", "game", "fancyband")
			if _, err := os.Stat(built); err == nil {
				return checkEngine(built)
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return "", errors.New("cannot find the fancyband program; pass -engine PATH or set FANCYBAND_BIN")
}

// findDefaults looks for the source tree's folder of default tiles, for
// developer mode: in a checkout above the engine, as scripts/check builds
// it, or above the folder the client runs in.
func findDefaults(engine string) (string, error) {
	starts := []string{filepath.Dir(engine)}
	if dir, err := os.Getwd(); err == nil {
		starts = append(starts, dir)
	}
	for _, dir := range starts {
		for ; ; dir = filepath.Dir(dir) {
			tiles := filepath.Join(dir, "engine", "lib", "tiles")
			if _, err := os.Stat(filepath.Join(tiles, "list.txt")); err == nil {
				return filepath.Join(tiles, "fancyband"), nil
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	return "", errors.New("-dev needs the source tree: run it from a Fancyband checkout, or with an engine built in one")
}

func checkEngine(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not a program", abs)
	}
	return abs, nil
}
