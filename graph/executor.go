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

type branchResult struct {
	idx   int
	state State
}

// execution encapsulates the mutable state for a single graph run.
type task struct {
	executor    *Executor
	queue       []Step
	pending     map[string]State
	visited     map[string]bool
	finished    bool
	finishState State
	stepCount   int
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

// Execute runs the graph execution starting from the given state.
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
		pending: make(map[string]State),
		visited: make(map[string]bool, len(e.graph.nodes)),
	}
}

func (x *task) execute(ctx context.Context) (State, error) {
	for len(x.queue) > 0 {
		if x.stepCount >= x.executor.graph.maxSteps {
			return nil, fmt.Errorf("graph: exceeded maximum steps limit (%d)", x.executor.graph.maxSteps)
		}

		step := x.dequeue()
		if x.shouldSkip(&step) {
			continue
		}

		x.stepCount++

		nextState, err := x.executeNode(ctx, step)
		if err != nil {
			return nil, err
		}
		if x.handleFinish(step.node, nextState) {
			continue
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

	if step.waitAllPreds {
		ready := x.predecessorsReady(step.node)
		if ready {
			for _, queued := range x.queue {
				if queued.node == step.node && !queued.allowRevisit {
					ready = false
					break
				}
			}
		}
		if !ready {
			x.pending[step.node] = mergeStates(x.pending[step.node], step.state)
			return true
		}
	}

	if pendingState, exists := x.pending[step.node]; exists {
		step.state = mergeStates(pendingState, step.state)
		delete(x.pending, step.node)
	}

	return false
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

func (x *task) handleFinish(node string, state State) bool {
	x.finishState = state.Clone()
	if node == x.executor.graph.finishPoint {
		x.finished = true
		return true
	}
	return false
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
		x.fanOutSerial(step, edges)
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
	conditionalEdges, unconditionalEdges := x.classifyEdges(edges)
	if len(conditionalEdges) == 0 {
		return edgeResolution{fanOut: edges}, nil
	}
	if len(unconditionalEdges) == 0 {
		return x.resolveAllConditional(ctx, state, conditionalEdges, step.node)
	}
	return x.resolveMixed(ctx, state, edges, step.node)
}

func (x *task) classifyEdges(edges []conditionalEdge) (conditional, unconditional []conditionalEdge) {
	for _, edge := range edges {
		if edge.condition != nil {
			conditional = append(conditional, edge)
		} else {
			unconditional = append(unconditional, edge)
		}
	}
	return
}

func (x *task) resolveAllConditional(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
	matched := make([]conditionalEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.condition(ctx, state) {
			matched = append(matched, edge)
		}
	}
	if len(matched) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
	}
	if len(matched) == 1 {
		return edgeResolution{
			immediate: []Step{{
				node:         matched[0].to,
				state:        state.Clone(),
				allowRevisit: true,
				waitAllPreds: x.shouldWaitForNode(matched[0].to),
			}},
		}, nil
	}
	return edgeResolution{fanOut: matched}, nil
}

func (x *task) resolveMixed(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			return edgeResolution{
				immediate: []Step{{
					node:         edge.to,
					state:        state.Clone(),
					allowRevisit: true,
					waitAllPreds: x.shouldWaitForNode(edge.to),
				}},
				prepend: true,
			}, nil
		}
	}
	return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
}

func (x *task) fanOutSerial(step Step, edges []conditionalEdge) {
	for _, edge := range edges {
		x.enqueue(Step{
			node:         edge.to,
			state:        nil,
			allowRevisit: step.allowRevisit,
			waitAllPreds: false,
		})
	}
}

func (x *task) fanOutParallel(ctx context.Context, step Step, state State, edges []conditionalEdge) (State, error) {
	results := make([]branchResult, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)
	for i, edge := range edges {
		i := i
		edge := edge
		eg.Go(func() error {
			handler := x.executor.graph.nodes[edge.to]
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

	mergedBranches := state.Clone()
	successorStates := make(map[string]State)

	for _, result := range results {
		edge := edges[result.idx]
		x.visited[edge.to] = true
		mergedBranches = mergeStates(mergedBranches, result.state)

		for _, nextEdge := range x.executor.graph.edges[edge.to] {
			successorStates[nextEdge.to] = mergeStates(successorStates[nextEdge.to], result.state)
		}
	}

	for successor, successorState := range successorStates {
		x.enqueue(Step{
			node:         successor,
			state:        successorState.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: true,
		})
	}

	return mergedBranches, nil
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
