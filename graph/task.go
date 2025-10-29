package graph

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Step represents a single execution step in the graph.
type Step struct {
	node         string
	state        State
	allowRevisit bool
	waitAllPreds bool // if true, wait for all predecessors to be visited
}

// edgeResolution encapsulates the result of edge resolution.
type edgeResolution struct {
	immediate []Step
	fanOut    []conditionalEdge
	prepend   bool
}

// Task execution encapsulates the mutable state for a single graph run.
type Task struct {
	executor    *Executor
	queue       []Step
	pending     map[string]State
	visited     map[string]bool
	skippedCnt  map[string]int
	skippedFrom map[string]map[string]bool
	finished    bool
	finishState State
}

func (t *Task) execute(ctx context.Context) (State, error) {
	for len(t.queue) > 0 {
		step := t.dequeue()
		if t.shouldSkip(&step) {
			continue
		}

		nextState, err := t.executeNode(ctx, step)
		if err != nil {
			return nil, err
		}

		// Update finish state and check if we're done
		t.finishState = nextState.Clone()
		if step.node == t.executor.graph.finishPoint {
			t.finished = true
			break
		}

		if err := t.processOutgoingEdges(ctx, step, nextState); err != nil {
			return nil, err
		}
	}

	if t.finished {
		return t.finishState, nil
	}
	return nil, fmt.Errorf("graph: finish node not reachable: %s", t.executor.graph.finishPoint)
}

func (t *Task) dequeue() Step {
	step := t.queue[0]
	t.queue = t.queue[1:]
	return step
}

func (t *Task) shouldSkip(step *Step) bool {
	if t.visited[step.node] && !step.allowRevisit {
		return true
	}

	if step.waitAllPreds && !t.allPredsReady(step.node) {
		t.pending[step.node] = mergeStates(t.pending[step.node], step.state)
		return true
	}

	if pendingState, exists := t.pending[step.node]; exists {
		step.state = mergeStates(pendingState, step.state)
		delete(t.pending, step.node)
	}

	return false
}

// allPredsReady checks if all predecessors are ready and no duplicate is in the queue.
func (t *Task) allPredsReady(node string) bool {
	if !t.predecessorsReady(node) {
		return false
	}
	// Check if there's a duplicate in the queue that shouldn't be revisited
	for _, queued := range t.queue {
		if queued.node == node && !queued.allowRevisit {
			return false
		}
	}
	return true
}

func (t *Task) executeNode(ctx context.Context, step Step) (State, error) {
	state := t.stateFor(step)
	handler := t.executor.graph.nodes[step.node]
	if handler == nil {
		return nil, fmt.Errorf("graph: node %s handler missing", step.node)
	}
	if len(t.executor.graph.middlewares) > 0 {
		handler = ChainMiddlewares(t.executor.graph.middlewares...)(handler)
	}
	nextState, err := handler(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("graph: node %s: %w", step.node, err)
	}
	t.visited[step.node] = true

	return nextState.Clone(), nil
}

func (t *Task) stateFor(step Step) State {
	if step.state != nil {
		return step.state
	}
	return t.finishState
}

func (t *Task) processOutgoingEdges(ctx context.Context, step Step, state State) error {
	resolution, err := t.resolveEdges(ctx, step, state)
	if err != nil {
		return err
	}
	if len(resolution.immediate) > 0 {
		t.enqueueSteps(resolution.immediate, resolution.prepend)
		return nil
	}
	edges := resolution.fanOut
	if !t.executor.graph.parallel {
		t.fanOutSerial(step, state, edges)
		return nil
	}
	if len(edges) == 1 {
		t.enqueue(Step{
			node:         edges[0].to,
			state:        state.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: step.waitAllPreds,
		})
		return nil
	}
	_, err = t.fanOutParallel(ctx, step, state, edges)
	if err != nil {
		return err
	}
	return nil
}

func (t *Task) enqueue(step Step) {
	t.queue = append(t.queue, step)
}

func (t *Task) enqueueSteps(steps []Step, prepend bool) {
	if len(steps) == 0 {
		return
	}
	if prepend {
		t.queue = append(steps, t.queue...)
		return
	}
	t.queue = append(t.queue, steps...)
}

func (t *Task) resolveEdges(ctx context.Context, step Step, state State) (edgeResolution, error) {
	edges := t.executor.graph.edges[step.node]
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

	t.registerSkippedTargets(step.node, skipped)

	// Single matched edge: execute immediately
	if len(matched) == 1 {
		return edgeResolution{
			immediate: []Step{{
				node:         matched[0].to,
				state:        state.Clone(),
				allowRevisit: true,
				waitAllPreds: t.shouldWaitForNode(matched[0].to),
			}},
			prepend: hasUnconditional,
		}, nil
	}

	// Multiple matched edges: fan out
	return edgeResolution{fanOut: matched}, nil
}

func (t *Task) fanOutSerial(step Step, current State, edges []conditionalEdge) {
	for _, edge := range edges {
		waitAllPreds := t.shouldWaitForNode(edge.to)
		t.enqueue(Step{
			node:         edge.to,
			state:        current.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: waitAllPreds,
		})
	}
}

func (t *Task) fanOutParallel(ctx context.Context, step Step, state State, edges []conditionalEdge) (State, error) {
	type branchState struct {
		to    string
		state State
	}

	states := make([]branchState, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)

	for i, edge := range edges {
		eg.Go(func() error {
			handler := t.executor.graph.nodes[edge.to]
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
		t.visited[bs.to] = true
		for _, nextEdge := range t.executor.graph.edges[bs.to] {
			successors[nextEdge.to] = mergeStates(successors[nextEdge.to], bs.state)
		}
	}

	// Enqueue successor nodes
	for successor, successorState := range successors {
		t.enqueue(Step{
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

func (t *Task) predecessorsReady(node string) bool {
	for _, pred := range t.executor.predecessors[node] {
		if pred == node {
			continue
		}
		if !t.visited[pred] {
			return false
		}
	}
	return true
}

func (t *Task) shouldWaitForNode(node string) bool {
	activePreds := 0
	for _, pred := range t.executor.predecessors[node] {
		if pred == node {
			continue
		}
		if !t.visited[pred] {
			return true
		}
		activePreds++
		if activePreds > 1 {
			return true
		}
	}
	return false
}

func (t *Task) registerSkippedTargets(parent string, targets []string) {
	for _, target := range targets {
		t.registerSkip(parent, target)
	}
}

func (t *Task) registerSkip(parent, target string) {
	preds := t.executor.predecessors[target]
	if len(preds) == 0 {
		return
	}
	if t.visited[target] {
		return
	}
	if t.skippedFrom[target] == nil {
		t.skippedFrom[target] = make(map[string]bool)
	}
	if t.skippedFrom[target][parent] {
		return
	}
	t.skippedFrom[target][parent] = true
	t.skippedCnt[target]++
	if t.skippedCnt[target] >= len(preds) {
		t.markNodeSkipped(target)
	}
}

func (t *Task) markNodeSkipped(node string) {
	if t.visited[node] {
		return
	}
	t.visited[node] = true
	delete(t.pending, node)
	for _, edge := range t.executor.graph.edges[node] {
		t.registerSkip(node, edge.to)
	}
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
