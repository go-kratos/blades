package blades

import (
	"github.com/go-kratos/generics"
)

// State holds the state of a session.
type State struct {
	generics.Map[string, any]
}
