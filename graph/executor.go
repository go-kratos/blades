package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

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

// ExecuteOption configures a single execution run.
type ExecuteOption func(*executeConfig)

type executeConfig struct {
	taskID string
}

func generateTaskID() string {
	return uuid.NewString()
}

// WithTaskID sets the identifier used for checkpoint persistence during Execute.
// When omitted or empty, a task ID is generated automatically.
func WithTaskID(taskID string) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.taskID = taskID
	}
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
// If no taskID is provided (via WithTaskID), one is generated automatically.
// The returned string is the taskID used for checkpoint persistence and can be used to resume the task later via the Resume method.
func (e *Executor) Execute(ctx context.Context, state State, opts ...ExecuteOption) (string, error) {
	cfg := executeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.taskID == "" {
		cfg.taskID = generateTaskID()
	}
	state.ensure()
	t := newTask(e, state, e.checkpointer, cfg.taskID)
	_, err := t.run(ctx, nil)
	if err != nil {
		return cfg.taskID, err
	}
	return cfg.taskID, nil
}

// Resume continues a previously started task using the configured Checkpointer.
func (e *Executor) Resume(ctx context.Context, checkpointID string) (State, error) {
	if e.checkpointer == nil {
		return State{}, fmt.Errorf("graph: no checkpointer configured")
	}
	checkpoint, err := e.checkpointer.Resume(ctx, checkpointID)
	if err != nil {
		return State{}, fmt.Errorf("graph: failed to load checkpoint: %w", err)
	}
	t := newTask(e, State{}, e.checkpointer, checkpointID)
	return t.run(ctx, checkpoint.Clone())
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
