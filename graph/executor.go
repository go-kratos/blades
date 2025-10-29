package graph

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Executor represents a compiled graph ready for execution. It is safe for
// concurrent use; each Execute call runs on an isolated execution context.
type Executor struct {
	graph        *Graph
	predecessors map[string][]string
}

// Step represents a single execution step in the graph.
type Step struct {
	node         string
	state        State
	allowRevisit bool
	waitAllPreds bool // if true, wait for all predecessors to be visited
}

type edgeResolution struct {
	immediate []Step
	fanOut    []conditionalEdge
	prepend   bool
}

// execution encapsulates the mutable state for a single graph run.
type task struct {
	executor    *Executor
	queue       []Step
	pending     map[string]State
	visited     map[string]bool
	skippedCnt  map[string]int
	skippedFrom map[string]map[string]bool
	finished    bool
	finishState State
}

// NewExecutor creates a new Executor for the given graph.
func NewExecutor(g *Graph) *Executor {
	predecessors := make(map[string][]string)
	for from, edges := range g.edges {
		for _, edge := range edges {
			predecessors[edge.to] = append(predecessors[edge.to], from)
		}
	}

	return &Executor{
		graph:        g,
		predecessors: predecessors,
	}
}

// Execute runs the graph task starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	t := e.newTask(state)
	return t.execute(ctx)
}

func (e *Executor) newTask(initial State) *task {
	var entryState State
	if initial != nil {
		entryState = initial.Clone()
	}
	return &task{
		executor: e,
		queue: []Step{{
			node:  e.graph.entryPoint,
			state: entryState,
		}},
		pending:     make(map[string]State),
		visited:     make(map[string]bool, len(e.graph.nodes)),
		skippedCnt:  make(map[string]int, len(e.graph.nodes)),
		skippedFrom: make(map[string]map[string]bool, len(e.graph.nodes)),
	}
}

func (x *task) execute(ctx context.Context) (State, error) {
	for len(x.queue) > 0 {
		step := x.dequeue()
		if x.shouldSkip(&step) {
			continue
		}

		nextState, err := x.executeNode(ctx, step)
		if err != nil {
			return nil, err
		}

		// Update finish state and check if we're done
		x.finishState = nextState.Clone()
		if step.node == x.executor.graph.finishPoint {
			x.finished = true
			break
		}

		if err := x.processOutgoingEdges(ctx, step, nextState); err != nil {
			return nil, err
		}
	}

	if x.finished {
		return x.finishState, nil
	}
	return nil, fmt.Errorf("graph: finish node not reachable: %s", x.executor.graph.finishPoint)
}

func (x *task) dequeue() Step {
	step := x.queue[0]
	x.queue = x.queue[1:]
	return step
}

func (x *task) shouldSkip(step *Step) bool {
	if x.visited[step.node] && !step.allowRevisit {
		return true
	}

	if step.waitAllPreds && !x.allPredsReady(step.node) {
		x.pending[step.node] = mergeStates(x.pending[step.node], step.state)
		return true
	}

	if pendingState, exists := x.pending[step.node]; exists {
		step.state = mergeStates(pendingState, step.state)
		delete(x.pending, step.node)
	}

	return false
}

// allPredsReady checks if all predecessors are ready and no duplicate is in the queue.
func (x *task) allPredsReady(node string) bool {
	if !x.predecessorsReady(node) {
		return false
	}
	// Check if there's a duplicate in the queue that shouldn't be revisited
	for _, queued := range x.queue {
		if queued.node == node && !queued.allowRevisit {
			return false
		}
	}
	return true
}

func (x *task) executeNode(ctx context.Context, step Step) (State, error) {
	state := x.stateFor(step)
	handler := x.executor.graph.nodes[step.node]
	if handler == nil {
		return nil, fmt.Errorf("graph: node %s handler missing", step.node)
	}
	if len(x.executor.graph.middlewares) > 0 {
		handler = ChainMiddlewares(x.executor.graph.middlewares...)(handler)
	}
	nextState, err := handler(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("graph: node %s: %w", step.node, err)
	}
	x.visited[step.node] = true

	return nextState.Clone(), nil
}

func (x *task) stateFor(step Step) State {
	if step.state != nil {
		return step.state
	}
	return x.finishState
}

func (x *task) processOutgoingEdges(ctx context.Context, step Step, state State) error {
	resolution, err := x.resolveEdges(ctx, step, state)
	if err != nil {
		return err
	}
	if len(resolution.immediate) > 0 {
		x.enqueueSteps(resolution.immediate, resolution.prepend)
		return nil
	}
	edges := resolution.fanOut
	if !x.executor.graph.parallel {
		x.fanOutSerial(step, state, edges)
		return nil
	}
	if len(edges) == 1 {
		x.enqueue(Step{
			node:         edges[0].to,
			state:        state.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: step.waitAllPreds,
		})
		return nil
	}
	_, err = x.fanOutParallel(ctx, step, state, edges)
	if err != nil {
		return err
	}
	return nil
}

func (x *task) enqueue(step Step) {
	x.queue = append(x.queue, step)
}

func (x *task) enqueueSteps(steps []Step, prepend bool) {
	if len(steps) == 0 {
		return
	}
	if prepend {
		x.queue = append(steps, x.queue...)
		return
	}
	x.queue = append(x.queue, steps...)
}

func (x *task) resolveEdges(ctx context.Context, step Step, state State) (edgeResolution, error) {
	edges := x.executor.graph.edges[step.node]
	if len(edges) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no outgoing edges from node %s", step.node)
	}

	// Check if all edges are unconditional - if so, fan out directly
	allUnconditional := true
	for _, edge := range edges {
		if edge.condition != nil {
			allUnconditional = false
			break
		}
	}
	if allUnconditional {
		return edgeResolution{fanOut: edges}, nil
	}

	// Evaluate conditional edges and collect matched/skipped
	var matched []conditionalEdge
	var skipped []string
	hasUnconditional := false

	for i, edge := range edges {
		if edge.condition == nil {
			// Unconditional edge in mixed mode: take it and skip all following edges
			matched = append(matched, edge)
			hasUnconditional = true
			for _, trailing := range edges[i+1:] {
				skipped = append(skipped, trailing.to)
			}
			break
		}

		// Conditional edge: evaluate condition
		if edge.condition(ctx, state) {
			matched = append(matched, edge)
			// Check if there's an unconditional edge following
			if i+1 < len(edges) {
				hasTrailingUnconditional := false
				for _, trailing := range edges[i+1:] {
					if trailing.condition == nil {
						hasTrailingUnconditional = true
						break
					}
				}
				if hasTrailingUnconditional {
					// Mixed mode: first match wins, skip rest
					for _, trailing := range edges[i+1:] {
						skipped = append(skipped, trailing.to)
					}
					hasUnconditional = true
					break
				}
			}
		} else {
			skipped = append(skipped, edge.to)
		}
	}

	if len(matched) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", step.node)
	}

	x.registerSkippedTargets(step.node, skipped)

	// Single matched edge: execute immediately
	if len(matched) == 1 {
		return edgeResolution{
			immediate: []Step{{
				node:         matched[0].to,
				state:        state.Clone(),
				allowRevisit: true,
				waitAllPreds: x.shouldWaitForNode(matched[0].to),
			}},
			prepend: hasUnconditional,
		}, nil
	}

	// Multiple matched edges: fan out
	return edgeResolution{fanOut: matched}, nil
}

func (x *task) fanOutSerial(step Step, current State, edges []conditionalEdge) {
	for _, edge := range edges {
		waitAllPreds := x.shouldWaitForNode(edge.to)
		x.enqueue(Step{
			node:         edge.to,
			state:        current.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: waitAllPreds,
		})
	}
}

func (x *task) fanOutParallel(ctx context.Context, step Step, state State, edges []conditionalEdge) (State, error) {
	type branchState struct {
		to    string
		state State
	}

	states := make([]branchState, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)

	for i, edge := range edges {
		i, edge := i, edge
		eg.Go(func() error {
			handler := x.executor.graph.nodes[edge.to]
			if handler == nil {
				return fmt.Errorf("graph: node %s handler missing", edge.to)
			}
			nextState, err := handler(egCtx, state.Clone())
			if err != nil {
				return fmt.Errorf("graph: node %s: %w", edge.to, err)
			}
			states[i] = branchState{to: edge.to, state: nextState.Clone()}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Mark all branch nodes as visited and collect successor states
	successors := make(map[string]State)
	for _, bs := range states {
		x.visited[bs.to] = true
		for _, nextEdge := range x.executor.graph.edges[bs.to] {
			successors[nextEdge.to] = mergeStates(successors[nextEdge.to], bs.state)
		}
	}

	// Enqueue successor nodes
	for successor, successorState := range successors {
		x.enqueue(Step{
			node:         successor,
			state:        successorState,
			allowRevisit: step.allowRevisit,
			waitAllPreds: true,
		})
	}

	// Merge all branch states for return
	merged := state.Clone()
	for _, bs := range states {
		merged = mergeStates(merged, bs.state)
	}
	return merged, nil
}

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

func (x *task) predecessorsReady(node string) bool {
	for _, pred := range x.executor.predecessors[node] {
		if pred == node {
			continue
		}
		if !x.visited[pred] {
			return false
		}
	}
	return true
}

func (x *task) shouldWaitForNode(node string) bool {
	activePreds := 0
	for _, pred := range x.executor.predecessors[node] {
		if pred == node {
			continue
		}
		if !x.visited[pred] {
			return true
		}
		activePreds++
		if activePreds > 1 {
			return true
		}
	}
	return false
}

func (x *task) registerSkippedTargets(parent string, targets []string) {
	for _, target := range targets {
		x.registerSkip(parent, target)
	}
}

func (x *task) registerSkip(parent, target string) {
	preds := x.executor.predecessors[target]
	if len(preds) == 0 {
		return
	}
	if x.visited[target] {
		return
	}
	if x.skippedFrom[target] == nil {
		x.skippedFrom[target] = make(map[string]bool)
	}
	if x.skippedFrom[target][parent] {
		return
	}
	x.skippedFrom[target][parent] = true
	x.skippedCnt[target]++
	if x.skippedCnt[target] >= len(preds) {
		x.markNodeSkipped(target)
	}
}

func (x *task) markNodeSkipped(node string) {
	if x.visited[node] {
		return
	}
	x.visited[node] = true
	delete(x.pending, node)
	for _, edge := range x.executor.graph.edges[node] {
		x.registerSkip(node, edge.to)
	}
}
