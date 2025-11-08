package graph

import (
	"context"
	"sort"
)

// nodeInfo contains precomputed information for a node to avoid runtime lookups.
type nodeInfo struct {
	outEdges         []conditionalEdge // Precomputed outgoing edges
	nonLoopEdges     []conditionalEdge // Outgoing edges that are not loops
	predecessors     []string          // Ordered list of predecessors
	parentIndex      map[string]int    // Deterministic index for each predecessor
	dependencies     int               // Number of dependencies (predecessor count)
	isFinish         bool              // Whether this is the finish node
	hasConditions    bool              // Whether outgoing edges carry conditions
	loopDependencies int               // Number of incoming loop edges
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
	dependencyCounts := make(map[string]int)
	loopDependencyCounts := make(map[string]int)

	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
			if edge.edgeType == EdgeTypeLoop {
				loopDependencyCounts[edge.to]++
				continue
			}
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
		nonLoopEdges := make([]conditionalEdge, 0, len(rawEdges))
		for _, edge := range rawEdges {
			if edge.condition != nil {
				hasConditions = true
			}
			if edge.edgeType != EdgeTypeLoop {
				nonLoopEdges = append(nonLoopEdges, edge)
			}
		}
		node := &nodeInfo{
			outEdges:         rawEdges,
			nonLoopEdges:     nonLoopEdges,
			predecessors:     predecessors[nodeName],
			parentIndex:      buildParentIndex(predecessors[nodeName]),
			dependencies:     dependencyCounts[nodeName],
			isFinish:         nodeName == g.finishPoint,
			hasConditions:    hasConditions,
			loopDependencies: loopDependencyCounts[nodeName],
		}
		nodeInfos[nodeName] = node
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

func buildParentIndex(parents []string) map[string]int {
	if len(parents) == 0 {
		return nil
	}
	index := make(map[string]int, len(parents))
	for i, parent := range parents {
		index[parent] = i
	}
	return index
}
