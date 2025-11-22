package graph

import (
	"context"
	"sort"
)

// nodeInfo contains precomputed information for a node to avoid runtime lookups.
type nodeInfo struct {
	outEdges           []conditionalEdge // Precomputed outgoing edges
	unconditionalDests []string          // Target names for unconditional edges
	predecessors       []string          // Ordered list of predecessors
	dependencies       int               // Number of dependencies (predecessor count)
	isFinish           bool              // Whether this is the finish node
	hasConditions      bool              // Whether outgoing edges carry conditions
}

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph     *Graph
	nodeInfos map[string]*nodeInfo // Precomputed node information
}

// ExecuteOption configures a single execution run.
type ExecuteOption func(*executeConfig)

type executeConfig struct {
	onCheckpoint func(Checkpoint)
	resume       *Checkpoint
}

// WithCheckpointCallback registers a hook to receive checkpoints when the
// scheduler reaches a quiescent point (no in-flight nodes).
func WithCheckpointCallback(cb func(Checkpoint)) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.onCheckpoint = cb
	}
}

// WithCheckpointResume resumes execution from a previously captured checkpoint.
// The checkpoint is cloned internally to avoid caller-side mutation.
func WithCheckpointResume(cp Checkpoint) ExecuteOption {
	cloned := cp.Clone()
	return func(cfg *executeConfig) {
		cfg.resume = &cloned
	}
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	// Build predecessors map for deterministic state aggregation
	predecessors := make(map[string][]string, len(g.nodes))
	dependencyCounts := make(map[string]int)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
			dependencyCounts[edge.to]++
		}
	}
	predecessors[g.entryPoint] = append([]string{entryContributionParent}, predecessors[g.entryPoint]...)
	// Sort predecessors for deterministic state aggregation
	for node, parents := range predecessors {
		sort.Strings(parents)
		predecessors[node] = parents
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
			predecessors:       predecessors[nodeName],
			dependencies:       dependencyCounts[nodeName],
			isFinish:           nodeName == g.finishPoint,
			hasConditions:      hasConditions,
		}
		nodeInfos[nodeName] = node
	}
	return &Executor{
		graph:     g,
		nodeInfos: nodeInfos,
	}
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State, opts ...ExecuteOption) (State, error) {
	cfg := executeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	t := newTask(e, cfg.onCheckpoint)
	return t.run(ctx, state, cfg.resume)
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
