package event

// Input is the sealed interface for all input events sent to an Agent.
type Input interface {
	input()
}

// Output is the sealed interface for all output events emitted by an Agent.
type Output interface {
	output()
}
