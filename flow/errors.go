package flow

import "errors"

var (
	ErrRouterRequired = errors.New("flow: router is required for routing agent")
)
