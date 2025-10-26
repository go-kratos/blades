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
	err         error // accumulated error for builder pattern
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
// Returns the graph for chaining. Check error with Compile().
func (g *Graph[S]) AddNode(name string, handler GraphHandler[S]) *Graph[S] {
	if g.err != nil {
		return g
	}
	if _, ok := g.nodes[name]; ok {
		g.err = fmt.Errorf("graph: node %s already exists", name)
		return g
	}
	g.nodes[name] = handler
	return g
}

// AddEdge adds a directed edge from one node to another. Options can configure the edge.
// Returns the graph for chaining. Check error with Compile().
func (g *Graph[S]) AddEdge(from, to string, opts ...EdgeOption[S]) *Graph[S] {
	if g.err != nil {
		return g
	}
	for _, edge := range g.edges[from] {
		if edge.to == to {
			g.err = fmt.Errorf("graph: edge from %s to %s already exists", from, to)
			return g
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
	return g
}

// SetEntryPoint marks a node as the entry point.
// Returns the graph for chaining. Check error with Compile().
func (g *Graph[S]) SetEntryPoint(start string) *Graph[S] {
	if g.err != nil {
		return g
	}
	if g.entryPoint != "" {
		g.err = fmt.Errorf("graph: entry point already set to %s", g.entryPoint)
		return g
	}
	g.entryPoint = start
	return g
}

// SetFinishPoint marks a node as the finish point.
// Returns the graph for chaining. Check error with Compile().
func (g *Graph[S]) SetFinishPoint(end string) *Graph[S] {
	if g.err != nil {
		return g
	}
	if g.finishPoint != "" {
		g.err = fmt.Errorf("graph: finish point already set to %s", g.finishPoint)
		return g
	}
	g.finishPoint = end
	return g
}

// validate ensures the graph configuration is correct before compiling.
func (g *Graph[S]) validate() error {
	if g.err != nil {
		return g.err
	}
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
		frame := e.dequeue()

		if e.shouldSkip(frame) {
			continue
		}

		nextState, err := e.executeNode(ctx, frame)
		if err != nil {
			return e.globalState, err
		}

		if e.handleFinish(frame.node, nextState) {
			continue
		}

		if err := e.processOutgoingEdges(ctx, frame, nextState); err != nil {
			return e.globalState, err
		}
	}

	if e.finished {
		return e.finishState, nil
	}
	return e.globalState, fmt.Errorf("graph: finish node not reachable: %s", e.graph.finishPoint)
}

func (e *graphExecutor[S]) dequeue() graphFrame[S] {
	frame := e.queue[0]
	e.queue = e.queue[1:]
	return frame
}

func (e *graphExecutor[S]) shouldSkip(frame graphFrame[S]) bool {
	// Defer if waiting for other edges
	if e.waiting[frame.node] > 0 && !frame.allowRevisit {
		e.queue = append(e.queue, frame)
		return true
	}
	// Skip if already visited
	if e.visited[frame.node] && !frame.allowRevisit {
		return true
	}
	return false
}

func (e *graphExecutor[S]) executeNode(ctx context.Context, frame graphFrame[S]) (S, error) {
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
	return nextState, nil
}

func (e *graphExecutor[S]) handleFinish(node string, state S) bool {
	if node == e.graph.finishPoint {
		e.finished = true
		e.finishState = state
		return true
	}
	return false
}

func (e *graphExecutor[S]) processOutgoingEdges(ctx context.Context, frame graphFrame[S], state S) error {
	resolution, err := e.resolveEdges(ctx, frame, state)
	if err != nil {
		return err
	}

	// Handle immediate transitions (single matched conditional edge)
	if len(resolution.immediate) > 0 {
		e.enqueueFrames(resolution.immediate, resolution.prepend)
		return nil
	}

	edges := resolution.fanOut

	// Serial mode: enqueue edges sequentially
	if !e.graph.parallel {
		e.fanOutSerial(frame, edges)
		return nil
	}

	// Single edge: no need for parallel execution
	if len(edges) == 1 {
		e.enqueue(graphFrame[S]{
			node:         edges[0].to,
			state:        state,
			hasState:     true,
			allowRevisit: frame.allowRevisit,
		})
		return nil
	}

	// Multiple edges: execute in parallel
	branchState, err := e.fanOutParallel(ctx, frame, state, edges)
	if err != nil {
		return err
	}
	e.globalState = branchState
	return nil
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

	// Classify edges: all conditional, all unconditional, or mixed
	conditionalEdges, unconditionalEdges := e.classifyEdges(edges)

	// Case 1: All edges are unconditional - fan out to all
	if len(conditionalEdges) == 0 {
		return edgeResolution[S]{fanOut: edges}, nil
	}

	// Case 2: All edges are conditional - evaluate and fan out to matches
	if len(unconditionalEdges) == 0 {
		return e.resolveAllConditional(ctx, state, conditionalEdges, frame.node)
	}

	// Case 3: Mixed edges - evaluate in order, first match wins (conditional or unconditional)
	return e.resolveMixed(ctx, state, edges)
}

// classifyEdges separates edges into conditional and unconditional
func (e *graphExecutor[S]) classifyEdges(edges []conditionalEdge[S]) (conditional, unconditional []conditionalEdge[S]) {
	for _, edge := range edges {
		if edge.condition != nil {
			conditional = append(conditional, edge)
		} else {
			unconditional = append(unconditional, edge)
		}
	}
	return
}

// resolveAllConditional handles the case where all edges are conditional
func (e *graphExecutor[S]) resolveAllConditional(ctx context.Context, state S, edges []conditionalEdge[S], nodeName string) (edgeResolution[S], error) {
	matched := make([]conditionalEdge[S], 0, len(edges))
	for _, edge := range edges {
		if edge.condition(ctx, state) {
			matched = append(matched, edge)
		}
	}
	if len(matched) == 0 {
		return edgeResolution[S]{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
	}
	// Single match - take it immediately
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
	// Multiple matches - fan out
	return edgeResolution[S]{fanOut: matched}, nil
}

// resolveMixed handles the case where edges are a mix of conditional and unconditional
// First match wins (conditional edges are checked first, then unconditional)
func (e *graphExecutor[S]) resolveMixed(ctx context.Context, state S, edges []conditionalEdge[S]) (edgeResolution[S], error) {
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
	return edgeResolution[S]{}, fmt.Errorf("graph: no condition matched for edges from node %s", edges[0].to)
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
