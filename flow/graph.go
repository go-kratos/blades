package flow

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// GraphHandler is a function that processes the graph state.
// Handlers must not mutate the incoming state; instead, they should return a new state instance.
// This is especially important for reference types (e.g., pointers, slices, maps) to avoid unintended side effects.
type GraphHandler func(ctx context.Context, state State) (State, error)

// EdgeCondition is a function that determines if an edge should be followed based on the current state.
type EdgeCondition func(ctx context.Context, state State) bool

// EdgeOption configures an edge before it is added to the graph.
type EdgeOption func(*conditionalEdge)

// WithEdgeCondition sets a condition that must return true for the edge to be taken.
func WithEdgeCondition(condition EdgeCondition) EdgeOption {
	return func(edge *conditionalEdge) {
		edge.condition = condition
	}
}

// conditionalEdge represents an edge with an optional condition.
type conditionalEdge struct {
	to        string
	condition EdgeCondition // nil means always follow this edge
}

// Graph represents a directed graph of processing nodes. Cycles are allowed.
type Graph struct {
	nodes       map[string]GraphHandler
	edges       map[string][]conditionalEdge
	entryPoint  string
	finishPoint string
	parallel    bool
	err         error // accumulated error for builder pattern
}

// Option configures the Graph behavior.
type Option func(*Graph)

// NewGraph creates a new empty Graph.
func NewGraph(opts ...Option) *Graph {
	g := &Graph{
		nodes:    make(map[string]GraphHandler),
		edges:    make(map[string][]conditionalEdge),
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
func (g *Graph) AddNode(name string, handler GraphHandler) *Graph {
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
func (g *Graph) AddEdge(from, to string, opts ...EdgeOption) *Graph {
	if g.err != nil {
		return g
	}
	for _, edge := range g.edges[from] {
		if edge.to == to {
			g.err = fmt.Errorf("graph: edge from %s to %s already exists", from, to)
			return g
		}
	}
	newEdge := conditionalEdge{to: to}
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
func (g *Graph) SetEntryPoint(start string) *Graph {
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
func (g *Graph) SetFinishPoint(end string) *Graph {
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
func (g *Graph) validate() error {
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
func (g *Graph) ensureReachable() error {
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

type graphFrame struct {
	node         string
	state        State
	hasState     bool
	allowRevisit bool
}

type edgeResolution struct {
	immediate []graphFrame
	fanOut    []conditionalEdge
	prepend   bool
}

type branchResult struct {
	idx   int
	state State
}

type graphExecutor struct {
	graph       *Graph
	queue       []graphFrame
	waiting     map[string]int
	visited     map[string]bool
	finished    bool
	finishState State
	globalState State
}

// Compile validates and compiles the graph into a GraphHandler.
// Nodes wait for all activated incoming edges to complete before executing (join semantics).
// An edge is "activated" when its source node executes and chooses that edge.
func (g *Graph) Compile() (GraphHandler, error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if err := g.ensureReachable(); err != nil {
		return nil, err
	}

	return func(ctx context.Context, state State) (State, error) {
		executor := newGraphExecutor(g, state)
		return executor.run(ctx)
	}, nil
}

func newGraphExecutor(g *Graph, state State) *graphExecutor {
	normalized := state.Clone()
	return &graphExecutor{
		graph:       g,
		queue:       []graphFrame{{node: g.entryPoint, state: normalized, hasState: true}},
		waiting:     make(map[string]int),
		visited:     make(map[string]bool, len(g.nodes)),
		globalState: normalized,
	}
}

func (e *graphExecutor) run(ctx context.Context) (State, error) {
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

func (e *graphExecutor) dequeue() graphFrame {
	frame := e.queue[0]
	e.queue = e.queue[1:]
	return frame
}

func (e *graphExecutor) shouldSkip(frame graphFrame) bool {
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

func (e *graphExecutor) executeNode(ctx context.Context, frame graphFrame) (State, error) {
	localState := e.stateFor(frame).Clone()
	handler := e.graph.nodes[frame.node]
	if handler == nil {
		return e.globalState, fmt.Errorf("graph: node %s handler missing", frame.node)
	}

	nextState, err := handler(ctx, localState)
	if err != nil {
		return e.globalState, fmt.Errorf("graph: node %s: %w", frame.node, err)
	}

	e.visited[frame.node] = true
	e.globalState = nextState.Clone()
	return nextState.Clone(), nil
}

func (e *graphExecutor) handleFinish(node string, state State) bool {
	if node == e.graph.finishPoint {
		e.finished = true
		e.finishState = state.Clone()
		return true
	}
	return false
}

func (e *graphExecutor) processOutgoingEdges(ctx context.Context, frame graphFrame, state State) error {
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
		e.enqueue(graphFrame{
			node:         edges[0].to,
			state:        state.Clone(),
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
	e.globalState = branchState.Clone()
	return nil
}

func (e *graphExecutor) stateFor(frame graphFrame) State {
	if frame.hasState {
		return frame.state
	}
	return e.globalState
}

func (e *graphExecutor) enqueue(frame graphFrame) {
	e.queue = append(e.queue, frame)
}

func (e *graphExecutor) enqueueFrames(frames []graphFrame, prepend bool) {
	if len(frames) == 0 {
		return
	}
	if prepend {
		e.queue = append(frames, e.queue...)
		return
	}
	e.queue = append(e.queue, frames...)
}

func (e *graphExecutor) resolveEdges(ctx context.Context, frame graphFrame, state State) (edgeResolution, error) {
	edges := e.graph.edges[frame.node]
	if len(edges) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no outgoing edges from node %s", frame.node)
	}

	// Classify edges: all conditional, all unconditional, or mixed
	conditionalEdges, unconditionalEdges := e.classifyEdges(edges)

	// Case 1: All edges are unconditional - fan out to all
	if len(conditionalEdges) == 0 {
		return edgeResolution{fanOut: edges}, nil
	}

	// Case 2: All edges are conditional - evaluate and fan out to matches
	if len(unconditionalEdges) == 0 {
		return e.resolveAllConditional(ctx, state, conditionalEdges, frame.node)
	}

	// Case 3: Mixed edges - evaluate in order, first match wins (conditional or unconditional)
	return e.resolveMixed(ctx, state, edges)
}

// classifyEdges separates edges into conditional and unconditional
func (e *graphExecutor) classifyEdges(edges []conditionalEdge) (conditional, unconditional []conditionalEdge) {
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
func (e *graphExecutor) resolveAllConditional(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
	matched := make([]conditionalEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.condition(ctx, state) {
			matched = append(matched, edge)
		}
	}
	if len(matched) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
	}
	// Single match - take it immediately
	if len(matched) == 1 {
		return edgeResolution{
			immediate: []graphFrame{{
				node:         matched[0].to,
				state:        state.Clone(),
				hasState:     true,
				allowRevisit: true,
			}},
		}, nil
	}
	// Multiple matches - fan out
	return edgeResolution{fanOut: matched}, nil
}

// resolveMixed handles the case where edges are a mix of conditional and unconditional
// First match wins (conditional edges are checked first, then unconditional)
func (e *graphExecutor) resolveMixed(ctx context.Context, state State, edges []conditionalEdge) (edgeResolution, error) {
	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			return edgeResolution{
				immediate: []graphFrame{{
					node:         edge.to,
					state:        state.Clone(),
					hasState:     true,
					allowRevisit: true,
				}},
				prepend: true,
			}, nil
		}
	}
	return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", edges[0].to)
}

func (e *graphExecutor) fanOutSerial(frame graphFrame, edges []conditionalEdge) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}
	for _, edge := range edges {
		e.waiting[edge.to]--
		if e.waiting[edge.to] == 0 {
			e.enqueue(graphFrame{
				node:         edge.to,
				hasState:     false,
				allowRevisit: frame.allowRevisit,
			})
		}
	}
}

func (e *graphExecutor) fanOutParallel(ctx context.Context, frame graphFrame, state State, edges []conditionalEdge) (State, error) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}

	for _, edge := range edges {
		for _, nextEdge := range e.graph.edges[edge.to] {
			e.waiting[nextEdge.to]++
		}
	}

	results := make([]branchResult, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)
	for i, edge := range edges {
		i := i
		edge := edge
		eg.Go(func() error {
			handler := e.graph.nodes[edge.to]
			if handler == nil {
				return fmt.Errorf("graph: node %s handler missing", edge.to)
			}

			nextState, err := handler(egCtx, state.Clone())
			if err != nil {
				return fmt.Errorf("graph: node %s: %w", edge.to, err)
			}
			results[i] = branchResult{idx: i, state: nextState.Clone()}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return e.globalState, err
	}

	successorStates := make(map[string]State)
	pending := make(map[string]State)
	mergedBranches := state.Clone()
	for _, result := range results {
		edge := edges[result.idx]
		e.waiting[edge.to]--

		branchEdges := e.graph.edges[edge.to]
		for _, nextEdge := range branchEdges {
			e.waiting[nextEdge.to]--
			pending[nextEdge.to] = mergeStates(pending[nextEdge.to], result.state)
			if e.waiting[nextEdge.to] == 0 {
				successorStates[nextEdge.to] = pending[nextEdge.to].Clone()
				delete(pending, nextEdge.to)
			}
		}

		mergedBranches = mergeStates(mergedBranches, result.state)
		e.visited[edge.to] = true
	}

	for successor, successorState := range successorStates {
		e.enqueue(graphFrame{
			node:         successor,
			state:        successorState.Clone(),
			hasState:     true,
			allowRevisit: frame.allowRevisit,
		})
	}

	return mergedBranches, nil
}

// WithParallel toggles parallel fan-out execution. Defaults to true.
func WithParallel(enabled bool) Option {
	return func(g *Graph) {
		g.parallel = enabled
	}
}
