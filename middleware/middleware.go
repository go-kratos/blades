package middleware

import "github.com/go-kratos/blades/event"

// InputMiddleware transforms the input event stream.
type InputMiddleware func(<-chan event.Input) <-chan event.Input

// OutputMiddleware transforms the output event stream.
type OutputMiddleware func(<-chan event.Output) <-chan event.Output
