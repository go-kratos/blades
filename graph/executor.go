package graph

import "context"

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
	graph     *Graph
	nodeInfos map[string]*nodeInfo // Precomputed node information
}

// ExecuteOption configures a single execution run.
type ExecuteOption func(*executeConfig)

type executeConfig struct {
	saver  CheckpointSaver
	resume *Checkpoint
}

// WithCheckpointSaver registers a saver to persist checkpoints when the
// scheduler reaches a quiescent point (no in-flight nodes).
func WithCheckpointSaver(saver CheckpointSaver) ExecuteOption {
	return func(cfg *executeConfig) {
		cfg.saver = saver
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
	state.ensure()
	t := newTask(e, state, cfg.saver)
	return t.run(ctx, cfg.resume)
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
