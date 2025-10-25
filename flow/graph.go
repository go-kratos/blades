package flow

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// GraphHandler is a function that processes the graph state.
// Handlers must not mutate the incoming state; instead, they should return a new state instance.
// This is especially important for reference types (e.g., pointers, slices, maps) to avoid unintended side effects.
type GraphHandler[S any] func(ctx context.Context, state S) (S, error)

// EdgeCondition is a function that determines if an edge should be followed based on the current state.
type EdgeCondition[S any] func(ctx context.Context, state S) bool

// EdgeOption configures an edge before it is added to the graph.
type EdgeOption[S any] func(*conditionalEdge[S])

// WithEdgeCondition sets a condition that must return true for the edge to be taken.
func WithEdgeCondition[S any](condition EdgeCondition[S]) EdgeOption[S] {
	return func(edge *conditionalEdge[S]) {
		edge.condition = condition
	}
}

// conditionalEdge represents an edge with an optional condition.
type conditionalEdge[S any] struct {
	to        string
	condition EdgeCondition[S] // nil means always follow this edge
}

// Graph represents a directed graph of processing nodes. Cycles are allowed.
type Graph[S any] struct {
	nodes       map[string]GraphHandler[S]
	edges       map[string][]conditionalEdge[S]
	entryPoint  string
	finishPoint string
	parallel    bool
}

// Option configures the Graph behavior.
type Option[S any] func(*Graph[S])

// NewGraph creates a new empty Graph.
func NewGraph[S any](opts ...Option[S]) *Graph[S] {
	g := &Graph[S]{
		nodes:    make(map[string]GraphHandler[S]),
		edges:    make(map[string][]conditionalEdge[S]),
		parallel: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

// AddNode adds a named node with its handler to the graph.
func (g *Graph[S]) AddNode(name string, handler GraphHandler[S]) error {
	if _, ok := g.nodes[name]; ok {
		return fmt.Errorf("graph: node %s already exists", name)
	}
	g.nodes[name] = handler
	return nil
}

// AddEdge adds a directed edge from one node to another. Options can configure the edge.
func (g *Graph[S]) AddEdge(from, to string, opts ...EdgeOption[S]) error {
	for _, edge := range g.edges[from] {
		if edge.to == to {
			return fmt.Errorf("graph: edge from %s to %s already exists", from, to)
		}
	}
	newEdge := conditionalEdge[S]{to: to}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&newEdge)
	}
	g.edges[from] = append(g.edges[from], newEdge)
	return nil
}

// SetEntryPoint marks a node as the entry point.
func (g *Graph[S]) SetEntryPoint(start string) error {
	if g.entryPoint != "" {
		return fmt.Errorf("graph: entry point already set to %s", g.entryPoint)
	}
	g.entryPoint = start
	return nil
}

// SetFinishPoint marks a node as the finish point.
func (g *Graph[S]) SetFinishPoint(end string) error {
	if g.finishPoint != "" {
		return fmt.Errorf("graph: finish point already set to %s", g.finishPoint)
	}
	g.finishPoint = end
	return nil
}

// validate ensures the graph configuration is correct before compiling.
func (g *Graph[S]) validate() error {
	if g.entryPoint == "" {
		return fmt.Errorf("graph: entry point not set")
	}
	if g.finishPoint == "" {
		return fmt.Errorf("graph: finish point not set")
	}
	if _, ok := g.nodes[g.entryPoint]; !ok {
		return fmt.Errorf("graph: start node not found: %s", g.entryPoint)
	}
	if _, ok := g.nodes[g.finishPoint]; !ok {
		return fmt.Errorf("graph: end node not found: %s", g.finishPoint)
	}
	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("graph: edge from unknown node: %s", from)
		}
		for _, edge := range edges {
			if _, ok := g.nodes[edge.to]; !ok {
				return fmt.Errorf("graph: edge to unknown node: %s", edge.to)
			}
		}
	}
	return nil
}

// ensureReachable verifies that the finish node can be reached from the entry node.
func (g *Graph[S]) ensureReachable() error {
	if g.entryPoint == g.finishPoint {
		return nil
	}
	queue := []string{g.entryPoint}
	visited := make(map[string]bool, len(g.nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		if node == g.finishPoint {
			return nil
		}
		for _, edge := range g.edges[node] {
			queue = append(queue, edge.to)
		}
	}
	return fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
}

// Compile validates and compiles the graph into a GraphHandler.
// Nodes wait for all activated incoming edges to complete before executing (join semantics).
// An edge is "activated" when its source node executes and chooses that edge.
func (g *Graph[S]) Compile() (GraphHandler[S], error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if err := g.ensureReachable(); err != nil {
		return nil, err
	}

	return func(ctx context.Context, state S) (S, error) {
		type frame struct {
			node         string
			state        S
			hasState     bool
			skipHandler  bool
			allowRevisit bool
		}

		// Track completed edges to implement join semantics
		type edgeKey struct {
			from, to string
		}
		completedEdges := make(map[edgeKey]bool)

		// Track how many incoming edges each node is waiting for
		waitingEdges := make(map[string]int)

		queue := []frame{{node: g.entryPoint, state: state, hasState: true}}
		visited := make(map[string]bool, len(g.nodes))
		var finalState S
		finishReached := false

		for len(queue) > 0 {
			currentFrame := queue[0]
			queue = queue[1:]
			current := currentFrame.node
			localState := currentFrame.state
			if !currentFrame.hasState {
				localState = state
			}

			// Check if we need to wait for more incoming edges
			if waitingEdges[current] > 0 && !currentFrame.allowRevisit {
				// This node is waiting for more predecessors, re-queue it
				queue = append(queue, currentFrame)
				continue
			}

			if visited[current] && !currentFrame.allowRevisit && !currentFrame.skipHandler {
				continue
			}
			if !currentFrame.skipHandler {
				visited[current] = true
				handler := g.nodes[current]
				if handler == nil {
					return state, fmt.Errorf("graph: node %s handler missing", current)
				}
				var err error
				localState, err = handler(ctx, localState)
				if err != nil {
					return state, fmt.Errorf("graph: node %s: %w", current, err)
				}
			}
			state = localState

			if current == g.finishPoint {
				finishReached = true
				finalState = localState
				continue
			}

			edges := g.edges[current]
			if len(edges) == 0 {
				return state, fmt.Errorf("graph: no outgoing edges from node %s", current)
			}

		hasConditional := false
		allConditional := true
		for _, edge := range edges {
			if edge.condition != nil {
				hasConditional = true
			} else {
				allConditional = false
			}
		}

		if hasConditional {
			if allConditional {
				// All edges are conditional: execute all that return true
				var matchedEdges []conditionalEdge[S]
				for _, edge := range edges {
					if edge.condition(ctx, localState) {
						matchedEdges = append(matchedEdges, edge)
					}
				}
				if len(matchedEdges) == 0 {
					return state, fmt.Errorf("graph: no condition matched for edges from node %s", current)
				}
				// For conditional edges, allow revisiting nodes (needed for loops)
				if len(matchedEdges) == 1 {
					queue = append(queue, frame{
						node:         matchedEdges[0].to,
						state:        localState,
						hasState:     true,
						skipHandler:  false,
						allowRevisit: true,
					})
					completedEdges[edgeKey{from: current, to: matchedEdges[0].to}] = true
					continue
				}
				// Process all matched edges
				edges = matchedEdges
				// Fall through to normal edge processing
			} else {
				// Mixed conditional and unconditional: take only the first match
				nextFrame := frame{allowRevisit: true}
				matched := false
				for _, edge := range edges {
					if edge.condition == nil || edge.condition(ctx, localState) {
						nextFrame.node = edge.to
						nextFrame.state = localState
						nextFrame.hasState = true
						nextFrame.skipHandler = false
						matched = true
						// Mark edge as completed
						completedEdges[edgeKey{from: current, to: edge.to}] = true
						break
					}
				}
				if !matched {
					return state, fmt.Errorf("graph: no condition matched for edges from node %s", current)
				}
				queue = append([]frame{nextFrame}, queue...)
				continue
			}
		}

			if !g.parallel {
				// Serial mode: first activate all edges, then process sequentially
				for _, edge := range edges {
					waitingEdges[edge.to]++
				}
				for _, edge := range edges {
					completedEdges[edgeKey{from: current, to: edge.to}] = true
					waitingEdges[edge.to]--

					if waitingEdges[edge.to] == 0 {
						queue = append(queue, frame{
							node:         edge.to,
							state:        state, // Use global state in serial mode
							hasState:     false, // Mark to use global state during execution
							skipHandler:  false,
							allowRevisit: currentFrame.allowRevisit,
						})
					}
				}
				continue
			}

			if len(edges) == 1 {
				queue = append(queue, frame{
					node:         edges[0].to,
					state:        localState,
					hasState:     true,
					skipHandler:  false,
					allowRevisit: currentFrame.allowRevisit,
				})
				continue
			}

			// Parallel mode: activate all edges first
			for _, edge := range edges {
				waitingEdges[edge.to]++
			}

			type branchResult struct {
				idx   int
				state S
			}
			results := make([]branchResult, len(edges))

			eg, egCtx := errgroup.WithContext(ctx)
			for i, edge := range edges {
				i := i
				edge := edge
				eg.Go(func() error {
					childHandler := g.nodes[edge.to]
					if childHandler == nil {
						return fmt.Errorf("graph: node %s handler missing", edge.to)
					}
					
				nextState, err := childHandler(egCtx, localState)
					if err != nil {
						return fmt.Errorf("graph: node %s: %w", edge.to, err)
					}
					results[i] = branchResult{idx: i, state: nextState}
					return nil
				})
			}

			if err := eg.Wait(); err != nil {
				return state, err
			}


		// Process results and their successors (winner takes all in parallel mode)
		winner := results[len(results)-1]
		state = winner.state

		// Collect all unique successors from all branches
		successorMap := make(map[string]S)
		for i, result := range results {
			edge := edges[result.idx]
			// Mark edge as completed
			completedEdges[edgeKey{from: current, to: edge.to}] = true
			waitingEdges[edge.to]--

			// Process this branch node's successors
			branchEdges := g.edges[edge.to]
			for _, nextEdge := range branchEdges {
				waitingEdges[nextEdge.to]++
			}

			for _, nextEdge := range branchEdges {
				completedEdges[edgeKey{from: edge.to, to: nextEdge.to}] = true
				waitingEdges[nextEdge.to]--

				if waitingEdges[nextEdge.to] == 0 {
					successorMap[nextEdge.to] = result.state
				}
			}

			// Mark branch node as visited
			visited[edge.to] = true

			// Update global state from winner
			if i == len(results)-1 {
				state = result.state
			}
		}

		// Add all ready successors to queue (deduplicated)
		for successor, successorState := range successorMap {
			queue = append(queue, frame{
				node:         successor,
				state:        successorState,
				hasState:     true,
				skipHandler:  false,
				allowRevisit: currentFrame.allowRevisit,
			})
		}
	}

	if finishReached {
		return finalState, nil
	}
	return state, fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
}, nil
}

// WithParallel toggles parallel fan-out execution. Defaults to true.
func WithParallel[S any](enabled bool) Option[S] {
	return func(g *Graph[S]) {
		g.parallel = enabled
	}
}
