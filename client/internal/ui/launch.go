package ui

import "strings"

// Launch says which character an engine starts on. Angband cannot unload
// one character and load another, so the client switches characters by
// starting a new engine. The zero Launch is the first start, with the
// engine options the player gave, less -n: the start menu makes new
// characters, so each gets a savefile of its own.
type Launch struct {
	// Name loads that savefile, as the engine's -u option does.
	Name string
	// New makes a new character in a new savefile named at birth.
	New bool
	// Menu starts with no particular character and opens the start menu,
	// as after a death.
	Menu bool
}

// first reports whether this is the first start.
func (l Launch) first() bool { return l == Launch{} }

// Args returns the engine's options and the net module's options for
// this launch. base holds the options the player gave; a later launch
// replaces any -n or -u in them with its own.
func (l Launch) Args(base []string) (args, module []string) {
	for _, a := range base {
		if l.first() {
			if a != "-n" {
				args = append(args, a)
			}
			continue
		}
		if a == "-n" || (l.Name != "" || l.New) && strings.HasPrefix(a, "-u") {
			continue
		}
		args = append(args, a)
	}
	switch {
	case l.New:
		args = append(args, "-n")
		module = []string{"--new-save"}
	case l.Name != "":
		args = append(args, "-u"+l.Name)
	}
	return args, module
}

// hasNew reports whether the options ask for a new character.
func hasNew(args []string) bool {
	for _, a := range args {
		if a == "-n" {
			return true
		}
	}
	return false
}

// hasName reports whether the options pick a savefile with -u.
func hasName(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-u") && len(a) > 2 {
			return true
		}
	}
	return false
}
