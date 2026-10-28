// Package ui holds everything in neoband that touches Limoni.
//
// Keeping every Limoni import in this one package means a Limoni upgrade
// changes only this package. See the risks section of
// docs/scenario-2-plan.md.
package ui

// This blank import pins the Limoni version in go.mod until Phase 3 adds the
// real code that uses it.
import _ "github.com/thebanri/limoni"
