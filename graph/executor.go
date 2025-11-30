package graph

import (
	"context"
	"fmt"
	"maps"
)

// ExecuteOption defines an option for the Execute method.
type ExecuteOption func(*executeOptions)

type executeOptions struct {
	CheckpointID string
}

// WithCheckpointID sets a specific CheckpointID for the execution.
func WithCheckpointID(CheckpointID string) ExecuteOption {
	return func(cfg *executeOptions) {
		cfg.CheckpointID = CheckpointID
	}
}

// nodeInfo contains precomputed information for a node to avoid runtime lookups.
type nodeInfo struct {
	outEdges           []conditionalEdge // Precomputed outgoing edges
	unconditionalDests []string          // Target names for unconditional edges
	dependencies       int               // Number of dependencies (predecessor count)
	isFinish           bool              // Whether this is the finish node
	hasConditions      bool              // Whether outgoing edges carry conditions
}

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph        *Graph
	nodeInfos    map[string]*nodeInfo // Precomputed node information
	checkpointer Checkpointer
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph, checkpointer Checkpointer) *Executor {
	dependencyCounts := make(map[string]int)
	for _, edges := range g.edges {
		for _, edge := range edges {
			dependencyCounts[edge.to]++
		}
	}
	// Build nodeInfo map with precomputed data
	nodeInfos := make(map[string]*nodeInfo, len(g.nodes))
	for nodeName := range g.nodes {
		rawEdges := cloneEdges(g.edges[nodeName])
		hasConditions := false
		unconditionalDests := make([]string, 0, len(rawEdges))
		for _, edge := range rawEdges {
			if edge.condition != nil {
				hasConditions = true
			} else {
				unconditionalDests = append(unconditionalDests, edge.to)
			}
		}
		node := &nodeInfo{
			outEdges:           rawEdges,
			unconditionalDests: unconditionalDests,
			dependencies:       dependencyCounts[nodeName],
			isFinish:           nodeName == g.finishPoint,
			hasConditions:      hasConditions,
		}
		nodeInfos[nodeName] = node
	}
	return &Executor{
		graph:        g,
		nodeInfos:    nodeInfos,
		checkpointer: checkpointer,
	}
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State, opts ...ExecuteOption) (State, error) {
	o := executeOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	t := newTask(e, state, e.checkpointer, o.CheckpointID)
	return t.run(ctx, nil)
}

// Resume continues a previously started task using the configured Checkpointer.
func (e *Executor) Resume(ctx context.Context, state State, opts ...ExecuteOption) (State, error) {
	o := executeOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	if e.checkpointer == nil {
		return nil, fmt.Errorf("graph: no checkpointer configured")
	}
	checkpoint, err := e.checkpointer.Resume(ctx, o.CheckpointID)
	if err != nil {
		return nil, fmt.Errorf("graph: failed to load checkpoint: %w", err)
	}
	// Merge checkpoint state with provided state (provided values override checkpoint)
	maps.Copy(state, checkpoint.State)
	task := newTask(e, state, e.checkpointer, o.CheckpointID)
	return task.run(ctx, checkpoint)
}

// cloneEdges creates a copy of edge slice to avoid shared state issues.
func cloneEdges(edges []conditionalEdge) []conditionalEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]conditionalEdge, len(edges))
	copy(out, edges)
	return out
}
