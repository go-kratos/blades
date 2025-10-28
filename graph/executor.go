package graph

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Executor represents a compiled graph ready for execution.
type Executor struct {
	graph       *Graph
	queue       []Step
	waiting     map[string]int
	visited     map[string]bool
	finished    bool
	finishState State
	stepCount   int // tracks total number of steps executed
}

// Step represents a single execution step in the graph.
type Step struct {
	node         string
	state        State
	allowRevisit bool
}

type edgeResolution struct {
	immediate []Step
	fanOut    []conditionalEdge
	prepend   bool
}

type branchResult struct {
	idx   int
	state State
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	return &Executor{
		graph:   g,
		queue:   []Step{{node: g.entryPoint}},
		waiting: make(map[string]int),
		visited: make(map[string]bool, len(g.nodes)),
	}
}

// Execute runs the graph execution starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	for len(e.queue) > 0 {
		// Check if we've exceeded the maximum number of steps
		if e.stepCount >= e.graph.maxSteps {
			return nil, fmt.Errorf("graph: exceeded maximum steps limit (%d)", e.graph.maxSteps)
		}

		step := e.dequeue()
		if e.shouldSkip(step) {
			continue
		}

		e.stepCount++

		nextState, err := e.executeNode(ctx, step)
		if err != nil {
			return nil, err
		}
		if e.handleFinish(step.node, nextState) {
			continue
		}
		if err := e.processOutgoingEdges(ctx, step, nextState); err != nil {
			return nil, err
		}
	}
	if e.finished {
		return e.finishState, nil
	}
	return nil, fmt.Errorf("graph: finish node not reachable: %s", e.graph.finishPoint)
}

func (e *Executor) dequeue() Step {
	step := e.queue[0]
	e.queue = e.queue[1:]
	return step
}

func (e *Executor) shouldSkip(step Step) bool {
	// Defer if waiting for other edges
	if e.waiting[step.node] > 0 && !step.allowRevisit {
		e.queue = append(e.queue, step)
		return true
	}
	// Skip if already visited
	if e.visited[step.node] && !step.allowRevisit {
		return true
	}
	return false
}

func (e *Executor) executeNode(ctx context.Context, step Step) (State, error) {
	state := e.stateFor(step)
	handler := e.graph.nodes[step.node]
	if handler == nil {
		return nil, fmt.Errorf("graph: node %s handler missing", step.node)
	}
	if len(e.graph.middlewares) > 0 {
		handler = ChainMiddlewares(e.graph.middlewares...)(handler)
	}
	nextState, err := handler(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("graph: node %s: %w", step.node, err)
	}
	e.visited[step.node] = true
	return nextState.Clone(), nil
}

func (e *Executor) stateFor(step Step) State {
	if step.state != nil {
		return step.state
	}
	return e.finishState
}

func (e *Executor) handleFinish(node string, state State) bool {
	e.finishState = state.Clone()
	if node == e.graph.finishPoint {
		e.finished = true
		return true
	}
	return false
}

func (e *Executor) processOutgoingEdges(ctx context.Context, step Step, state State) error {
	resolution, err := e.resolveEdges(ctx, step, state)
	if err != nil {
		return err
	}
	// Handle immediate transitions (single matched conditional edge)
	if len(resolution.immediate) > 0 {
		e.enqueueSteps(resolution.immediate, resolution.prepend)
		return nil
	}
	edges := resolution.fanOut
	// Serial mode: enqueue edges sequentially
	if !e.graph.parallel {
		e.fanOutSerial(step, edges)
		return nil
	}
	// Single edge: no need for parallel execution
	if len(edges) == 1 {
		e.enqueue(Step{
			node:         edges[0].to,
			state:        state.Clone(),
			allowRevisit: step.allowRevisit,
		})
		return nil
	}
	// Multiple edges: execute in parallel
	_, err = e.fanOutParallel(ctx, step, state, edges)
	if err != nil {
		return err
	}
	return nil
}

func (e *Executor) enqueue(step Step) {
	e.queue = append(e.queue, step)
}

func (e *Executor) enqueueSteps(steps []Step, prepend bool) {
	if len(steps) == 0 {
		return
	}
	if prepend {
		e.queue = append(steps, e.queue...)
		return
	}
	e.queue = append(e.queue, steps...)
}

func (e *Executor) resolveEdges(ctx context.Context, step Step, state State) (edgeResolution, error) {
	edges := e.graph.edges[step.node]
	if len(edges) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no outgoing edges from node %s", step.node)
	}
	// Classify edges: all conditional, all unconditional, or mixed
	conditionalEdges, unconditionalEdges := e.classifyEdges(edges)
	// Case 1: All edges are unconditional - fan out to all
	if len(conditionalEdges) == 0 {
		return edgeResolution{fanOut: edges}, nil
	}
	// Case 2: All edges are conditional - evaluate and fan out to matches
	if len(unconditionalEdges) == 0 {
		return e.resolveAllConditional(ctx, state, conditionalEdges, step.node)
	}
	// Case 3: Mixed edges - evaluate in order, first match wins (conditional or unconditional)
	return e.resolveMixed(ctx, state, edges, step.node)
}

// classifyEdges separates edges into conditional and unconditional
func (e *Executor) classifyEdges(edges []conditionalEdge) (conditional, unconditional []conditionalEdge) {
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
func (e *Executor) resolveAllConditional(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
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
			immediate: []Step{{
				node:         matched[0].to,
				state:        state.Clone(),
				allowRevisit: true,
			}},
		}, nil
	}
	// Multiple matches - fan out
	return edgeResolution{fanOut: matched}, nil
}

// resolveMixed handles the case where edges are a mix of conditional and unconditional
// First match wins (conditional edges are checked first, then unconditional)
func (e *Executor) resolveMixed(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			return edgeResolution{
				immediate: []Step{{
					node:         edge.to,
					state:        state.Clone(),
					allowRevisit: true,
				}},
				prepend: true,
			}, nil
		}
	}
	return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
}

func (e *Executor) fanOutSerial(step Step, edges []conditionalEdge) {
	for _, edge := range edges {
		e.enqueue(Step{
			node:         edge.to,
			state:        nil, // Use finishState for serial execution
			allowRevisit: step.allowRevisit,
		})
	}
}

func (e *Executor) fanOutParallel(ctx context.Context, step Step, state State, edges []conditionalEdge) (State, error) {
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
		return nil, err
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
		e.enqueue(Step{
			node:         successor,
			state:        successorState.Clone(),
			allowRevisit: step.allowRevisit,
		})
	}
	return mergedBranches, nil
}

// mergeStates merges states at the first level only.
// Each handler manages state at the key level, so we only merge top-level keys.
// Later updates override earlier values for the same key.
func mergeStates(base State, updates ...State) State {
	merged := State{}
	if base != nil {
		merged = base.Clone()
	}
	for _, update := range updates {
		if update == nil {
			continue
		}
		for k, v := range update {
			merged[k] = v
		}
	}
	return merged
}
