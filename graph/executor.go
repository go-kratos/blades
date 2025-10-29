package graph

import (
	"context"
	"fmt"
	"maps"

	"golang.org/x/sync/errgroup"
)

// Executor represents a compiled graph ready for execution.
type Executor struct {
	graph        *Graph
	queue        []Step
	predecessors map[string][]string // predecessors for each node
	pending      map[string]State    // nodes waiting for predecessors, with their accumulated state
	visited      map[string]bool
	finished     bool
	finishState  State
	stepCount    int // tracks total number of steps executed
}

// Step represents a single execution step in the graph.
type Step struct {
	node         string
	state        State
	allowRevisit bool
	waitAllPreds bool // if true, wait for all predecessors to be visited
}

type edgeResolution struct {
	steps   []Step
	fanOut  []conditionalEdge
	prepend bool
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	// Build predecessors map
	predecessors := make(map[string][]string)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
		}
	}

	return &Executor{
		graph:        g,
		queue:        []Step{{node: g.entryPoint}},
		predecessors: predecessors,
		pending:      make(map[string]State),
		visited:      make(map[string]bool, len(g.nodes)),
	}
}

// Execute runs the graph execution starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	e.reset(state)

	for len(e.queue) > 0 {
		// Check if we've exceeded the maximum number of steps
		if e.stepCount >= e.graph.maxSteps {
			return nil, fmt.Errorf("graph: exceeded maximum steps limit (%d)", e.graph.maxSteps)
		}

		step := e.dequeue()
		if e.shouldSkip(&step) {
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

func (e *Executor) reset(initial State) {
	var entryState State
	if initial != nil {
		entryState = initial.Clone()
		e.finishState = entryState.Clone()
	} else {
		entryState = nil
		e.finishState = nil
	}

	e.queue = []Step{{node: e.graph.entryPoint, state: entryState}}
	e.pending = make(map[string]State)
	e.visited = make(map[string]bool, len(e.graph.nodes))
	e.finished = false
	e.stepCount = 0
}

func (e *Executor) dequeue() Step {
	step := e.queue[0]
	e.queue = e.queue[1:]
	return step
}

func (e *Executor) shouldSkip(step *Step) bool {
	// Skip if already visited (unless revisit is allowed)
	if e.visited[step.node] && !step.allowRevisit {
		return true
	}

	if step.waitAllPreds {
		ready := e.predecessorsReady(step.node)
		if ready {
			for _, queued := range e.queue {
				if queued.node == step.node && !queued.allowRevisit {
					ready = false
					break
				}
			}
		}
		if !ready {
			e.pending[step.node] = mergeStates(e.pending[step.node], step.state)
			return true
		}
	}

	if pendingState, exists := e.pending[step.node]; exists {
		step.state = mergeStates(pendingState, step.state)
		delete(e.pending, step.node)
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
	if len(resolution.steps) > 0 {
		e.enqueueSteps(resolution.steps, resolution.prepend)
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
			waitAllPreds: step.waitAllPreds,
		})
		return nil
	}
	// Multiple edges: execute in parallel
	_, err = e.fanOutParallel(ctx, step, state, edges)
	return err
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
			steps: []Step{{
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
				steps: []Step{{
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
	// Execute all parallel branches concurrently
	results := make([]State, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)
	for i, edge := range edges {
		eg.Go(func() error {
			handler := e.graph.nodes[edge.to]
			if handler == nil {
				return fmt.Errorf("graph: node %s handler missing", edge.to)
			}
			nextState, err := handler(egCtx, state.Clone())
			if err != nil {
				return fmt.Errorf("graph: node %s: %w", edge.to, err)
			}
			results[i] = nextState.Clone()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Mark branches as visited and collect their successors
	mergedBranches := state.Clone()
	successorStates := make(map[string]State)

	for i, edge := range edges {
		// Mark this branch node as visited
		e.visited[edge.to] = true
		mergedBranches = mergeStates(mergedBranches, results[i])

		// Collect states for successor nodes
		for _, nextEdge := range e.graph.edges[edge.to] {
			successorStates[nextEdge.to] = mergeStates(successorStates[nextEdge.to], results[i])
		}
	}

	// Enqueue all successors with waitAllPreds=true so they wait for all predecessors
	for successor, successorState := range successorStates {
		e.enqueue(Step{
			node:         successor,
			state:        successorState.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: true, // Wait for all predecessors to complete
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
		if update != nil {
			maps.Copy(merged, update)
		}
	}
	return merged
}

func (e *Executor) predecessorsReady(node string) bool {
	for _, pred := range e.predecessors[node] {
		if pred == node {
			continue
		}
		if !e.visited[pred] {
			return false
		}
	}
	return true
}
