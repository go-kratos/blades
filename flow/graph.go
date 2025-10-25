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

type graphFrame[S any] struct {
	node         string
	state        S
	hasState     bool
	allowRevisit bool
}

type edgeResolution[S any] struct {
	immediate []graphFrame[S]
	fanOut    []conditionalEdge[S]
	prepend   bool
}

type branchResult[S any] struct {
	idx   int
	state S
}

type graphExecutor[S any] struct {
	graph       *Graph[S]
	queue       []graphFrame[S]
	waiting     map[string]int
	visited     map[string]bool
	finished    bool
	finishState S
	globalState S
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
		executor := newGraphExecutor(g, state)
		return executor.run(ctx)
	}, nil
}

func newGraphExecutor[S any](g *Graph[S], state S) *graphExecutor[S] {
	return &graphExecutor[S]{
		graph:       g,
		queue:       []graphFrame[S]{{node: g.entryPoint, state: state, hasState: true}},
		waiting:     make(map[string]int),
		visited:     make(map[string]bool, len(g.nodes)),
		globalState: state,
	}
}

func (e *graphExecutor[S]) run(ctx context.Context) (S, error) {
	for len(e.queue) > 0 {
		frame := e.queue[0]
		e.queue = e.queue[1:]

		if e.shouldDefer(frame) {
			continue
		}

		localState := e.stateFor(frame)
		handler := e.graph.nodes[frame.node]
		if handler == nil {
			return e.globalState, fmt.Errorf("graph: node %s handler missing", frame.node)
		}

		nextState, err := handler(ctx, localState)
		if err != nil {
			return e.globalState, fmt.Errorf("graph: node %s: %w", frame.node, err)
		}

		e.visited[frame.node] = true
		e.globalState = nextState

		if frame.node == e.graph.finishPoint {
			e.finished = true
			e.finishState = nextState
			continue
		}

		resolution, err := e.resolveEdges(ctx, frame, nextState)
		if err != nil {
			return e.globalState, err
		}

		if len(resolution.immediate) > 0 {
			e.enqueueFrames(resolution.immediate, resolution.prepend)
			continue
		}

		edges := resolution.fanOut
		if !e.graph.parallel {
			e.fanOutSerial(frame, edges)
			continue
		}

		if len(edges) == 1 {
			e.enqueue(graphFrame[S]{
				node:         edges[0].to,
				state:        nextState,
				hasState:     true,
				allowRevisit: frame.allowRevisit,
			})
			continue
		}

		branchState, err := e.fanOutParallel(ctx, frame, nextState, edges)
		if err != nil {
			return e.globalState, err
		}
		e.globalState = branchState
	}

	if e.finished {
		return e.finishState, nil
	}
	return e.globalState, fmt.Errorf("graph: finish node not reachable: %s", e.graph.finishPoint)
}

func (e *graphExecutor[S]) shouldDefer(frame graphFrame[S]) bool {
	if e.waiting[frame.node] > 0 && !frame.allowRevisit {
		e.queue = append(e.queue, frame)
		return true
	}
	if e.visited[frame.node] && !frame.allowRevisit {
		return true
	}
	return false
}

func (e *graphExecutor[S]) stateFor(frame graphFrame[S]) S {
	if frame.hasState {
		return frame.state
	}
	return e.globalState
}

func (e *graphExecutor[S]) enqueue(frame graphFrame[S]) {
	e.queue = append(e.queue, frame)
}

func (e *graphExecutor[S]) enqueueFrames(frames []graphFrame[S], prepend bool) {
	if len(frames) == 0 {
		return
	}
	if prepend {
		e.queue = append(frames, e.queue...)
		return
	}
	e.queue = append(e.queue, frames...)
}

func (e *graphExecutor[S]) resolveEdges(ctx context.Context, frame graphFrame[S], state S) (edgeResolution[S], error) {
	edges := e.graph.edges[frame.node]
	if len(edges) == 0 {
		return edgeResolution[S]{}, fmt.Errorf("graph: no outgoing edges from node %s", frame.node)
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

	if !hasConditional {
		return edgeResolution[S]{fanOut: edges}, nil
	}

	if allConditional {
		matched := make([]conditionalEdge[S], 0, len(edges))
		for _, edge := range edges {
			if edge.condition(ctx, state) {
				matched = append(matched, edge)
			}
		}
		if len(matched) == 0 {
			return edgeResolution[S]{}, fmt.Errorf("graph: no condition matched for edges from node %s", frame.node)
		}
		if len(matched) == 1 {
			return edgeResolution[S]{
				immediate: []graphFrame[S]{{
					node:         matched[0].to,
					state:        state,
					hasState:     true,
					allowRevisit: true,
				}},
			}, nil
		}
		return edgeResolution[S]{fanOut: matched}, nil
	}

	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			return edgeResolution[S]{
				immediate: []graphFrame[S]{{
					node:         edge.to,
					state:        state,
					hasState:     true,
					allowRevisit: true,
				}},
				prepend: true,
			}, nil
		}
	}

	return edgeResolution[S]{}, fmt.Errorf("graph: no condition matched for edges from node %s", frame.node)
}

func (e *graphExecutor[S]) fanOutSerial(frame graphFrame[S], edges []conditionalEdge[S]) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}
	for _, edge := range edges {
		e.waiting[edge.to]--
		if e.waiting[edge.to] == 0 {
			e.enqueue(graphFrame[S]{
				node:         edge.to,
				hasState:     false,
				allowRevisit: frame.allowRevisit,
			})
		}
	}
}

func (e *graphExecutor[S]) fanOutParallel(ctx context.Context, frame graphFrame[S], state S, edges []conditionalEdge[S]) (S, error) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}

	results := make([]branchResult[S], len(edges))
	eg, egCtx := errgroup.WithContext(ctx)
	for i, edge := range edges {
		i := i
		edge := edge
		eg.Go(func() error {
			handler := e.graph.nodes[edge.to]
			if handler == nil {
				return fmt.Errorf("graph: node %s handler missing", edge.to)
			}

			nextState, err := handler(egCtx, state)
			if err != nil {
				return fmt.Errorf("graph: node %s: %w", edge.to, err)
			}
			results[i] = branchResult[S]{idx: i, state: nextState}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return e.globalState, err
	}

	successorStates := make(map[string]S)
	for _, result := range results {
		edge := edges[result.idx]
		e.waiting[edge.to]--

		branchEdges := e.graph.edges[edge.to]
		for _, nextEdge := range branchEdges {
			e.waiting[nextEdge.to]++
		}
		for _, nextEdge := range branchEdges {
			e.waiting[nextEdge.to]--
			if e.waiting[nextEdge.to] == 0 {
				successorStates[nextEdge.to] = result.state
			}
		}

		e.visited[edge.to] = true
	}

	for successor, successorState := range successorStates {
		e.enqueue(graphFrame[S]{
			node:         successor,
			state:        successorState,
			hasState:     true,
			allowRevisit: frame.allowRevisit,
		})
	}

	return results[len(results)-1].state, nil
}

// WithParallel toggles parallel fan-out execution. Defaults to true.
func WithParallel[S any](enabled bool) Option[S] {
	return func(g *Graph[S]) {
		g.parallel = enabled
	}
}
