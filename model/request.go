package model

import "github.com/go-kratos/blades/tools"

// Request is the input to a model Provider.
type Request struct {
	Model    string
	System   string
	Messages []*Message
	Tools    []tools.ToolSpec
	Options  []Option
}
