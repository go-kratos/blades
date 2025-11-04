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
	depCount           int               // Number of dependencies (predecessor count)
	isFinish           bool              // Whether this is the finish node
	hasConditions      bool              // Whether outgoing edges carry conditions
}

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph     *Graph
	nodeInfos map[string]*nodeInfo // Precomputed node information
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	// Build predecessors map for deterministic state aggregation
	predecessors := make(map[string][]string, len(g.nodes))
	depCount := make(map[string]int)

	for node := range g.nodes {
		predecessors[node] = nil
	}

	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
			depCount[edge.to]++
		}
	}

	entry := g.entryPoint
	predecessors[entry] = append([]string{entryContributionParent}, predecessors[entry]...)

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

		info := &nodeInfo{
			outEdges:           rawEdges,
			unconditionalDests: unconditionalDests,
			predecessors:       predecessors[nodeName],
			depCount:           depCount[nodeName],
			isFinish:           nodeName == g.finishPoint,
			hasConditions:      hasConditions,
		}
		nodeInfos[nodeName] = info
	}

	return &Executor{
		graph:     g,
		nodeInfos: nodeInfos,
	}
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	t := newTask(e)
	return t.run(ctx, state)
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
