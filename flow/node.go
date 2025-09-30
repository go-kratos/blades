package flow

import "github.com/go-kratos/blades"

type DataType string

const (
	TypeString  DataType = "string"
	TypeNumber  DataType = "number"
	TypeBoolean DataType = "boolean"
	TypeObject  DataType = "object"
	TypeArray   DataType = "array"
)

type InputType struct {
	Type        DataType
	Name        string
	Value       string
	Description string
}

type OutputType struct {
	Type        DataType
	Name        string
	Value       string
	Description string
}

type NodeInput struct {
	Inputs []InputType
}

type NodeOutput struct {
	Outputs []OutputType
}

type NodeOption struct{}

type Transformer[I any] func(I) (I, error)

type Node blades.Runner[*NodeInput, *NodeOutput, NodeOption]
