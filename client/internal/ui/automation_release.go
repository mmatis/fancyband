//go:build !limoni_debug

package ui

import (
	"errors"

	"github.com/thebanri/limoni"
)

// withAutomation fails: the automation socket is only in builds tagged
// limoni_debug.
func withAutomation(m *Model, path string) (limoni.Model, func(*limoni.Program), func(), error) {
	return nil, nil, nil, errors.New("-automation needs a build with -tags limoni_debug")
}
