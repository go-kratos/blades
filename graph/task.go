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
	immediate []conditionalEdge
	fanOut    []conditionalEdge
	prepend   bool
}

// Task execution encapsulates the mutable state for a single graph run.
type Task struct {
	executor      *Executor
	queue         []Step
	pending       map[string]State
	visited       map[string]bool
	skippedCnt    map[string]int
	skippedFrom   map[string]map[string]bool
	remainingDeps map[string]map[string]int
	finished      bool
	finishState   State
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
		t.enqueueEdge(resolution.immediate[0], step, state, true, resolution.prepend)
		return nil
	}
	edges := resolution.fanOut
	if !t.executor.graph.parallel {
		t.fanOutSerial(step, state, edges)
		return nil
	}
	if len(edges) == 1 {
		t.enqueueEdge(edges[0], step, state, step.allowRevisit, false)
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
		return edgeResolution{fanOut: cloneEdges(edges)}, nil
	}

	// Evaluate conditional edges and collect matched/skipped
	var matched []conditionalEdge
	var skipped []conditionalEdge
	hasUnconditional := false

	for i, edge := range edges {
		if edge.condition == nil {
			matched = append(matched, edge)
			hasUnconditional = true
			for _, trailing := range edges[i+1:] {
				skipped = append(skipped, trailing)
			}
			break
		}

		if edge.condition(ctx, state) {
			matched = append(matched, edge)
			if i+1 < len(edges) {
				hasTrailingUnconditional := false
				for _, trailing := range edges[i+1:] {
					if trailing.condition == nil {
						hasTrailingUnconditional = true
						break
					}
				}
				if hasTrailingUnconditional {
					for _, trailing := range edges[i+1:] {
						skipped = append(skipped, trailing)
					}
					hasUnconditional = true
					break
				}
			}
		} else {
			skipped = append(skipped, edge)
		}
	}

	if len(matched) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", step.node)
	}

	t.registerSkippedTargets(step.node, skipped)

	// Single matched edge: execute immediately
	if len(matched) == 1 {
		return edgeResolution{
			immediate: matched,
			prepend:   hasUnconditional,
		}, nil
	}

	// Multiple matched edges: fan out
	return edgeResolution{fanOut: matched}, nil
}

func (t *Task) fanOutSerial(step Step, current State, edges []conditionalEdge) {
	for _, edge := range edges {
		t.enqueueEdge(edge, step, current, step.allowRevisit, false)
	}
}

func (t *Task) fanOutParallel(ctx context.Context, step Step, state State, edges []conditionalEdge) (State, error) {
	type branchState struct {
		to    string
		state State
	}

	states := make([]branchState, len(edges))
	readyIndices := make([]int, 0, len(edges))
	for i, edge := range edges {
		ready := t.prepareEdge(edge)
		if ready {
			readyIndices = append(readyIndices, i)
			continue
		}
		t.enqueuePreparedEdge(edge, step, state, step.allowRevisit, false, ready)
	}

	if len(readyIndices) == 0 {
		return state.Clone(), nil
	}

	eg, egCtx := errgroup.WithContext(ctx)

	for _, idx := range readyIndices {
		i := idx
		edge := edges[i]
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

	type successorAggregation struct {
		state State
		ready bool
	}
	successors := make(map[string]successorAggregation)

	for _, idx := range readyIndices {
		bs := states[idx]
		t.visited[bs.to] = true
		for _, nextEdge := range t.executor.graph.edges[bs.to] {
			ready := t.prepareEdge(nextEdge)
			agg := successors[nextEdge.to]
			agg.state = mergeStates(agg.state, bs.state)
			agg.ready = ready
			successors[nextEdge.to] = agg
		}
	}

	for successor, agg := range successors {
		nextStep := Step{
			node:         successor,
			state:        agg.state.Clone(),
			allowRevisit: step.allowRevisit,
			waitAllPreds: !agg.ready,
		}
		t.enqueue(nextStep)
	}

	merged := state.Clone()
	for _, idx := range readyIndices {
		merged = mergeStates(merged, states[idx].state)
	}
	return merged, nil
}

func (t *Task) predecessorsReady(node string) bool {
	return t.dependenciesSatisfied(node)
}

func (t *Task) shouldWaitForNode(node string) bool {
	return !t.dependenciesSatisfied(node)
}

func (t *Task) registerSkippedTargets(parent string, targets []conditionalEdge) {
	for _, edge := range targets {
		t.registerSkip(parent, edge)
	}
}

func (t *Task) registerSkip(parent string, edge conditionalEdge) {
	target := edge.to
	t.consumeDependency(target, edge.group)
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
		t.registerSkip(node, edge)
	}
}

func (t *Task) prepareEdge(edge conditionalEdge) bool {
	t.consumeDependency(edge.to, edge.group)
	return t.dependenciesSatisfied(edge.to)
}

func (t *Task) enqueueEdge(edge conditionalEdge, from Step, state State, allowRevisit bool, prepend bool) {
	ready := t.prepareEdge(edge)
	t.enqueuePreparedEdge(edge, from, state, allowRevisit, prepend, ready)
}

func (t *Task) enqueuePreparedEdge(edge conditionalEdge, from Step, state State, allowRevisit bool, prepend bool, ready bool) {
	next := Step{
		node:         edge.to,
		state:        state.Clone(),
		allowRevisit: allowRevisit,
		waitAllPreds: !ready,
	}
	if prepend {
		t.enqueueSteps([]Step{next}, true)
		return
	}
	t.enqueue(next)
}

func (t *Task) dependenciesSatisfied(node string) bool {
	groups, ok := t.remainingDeps[node]
	if !ok || len(groups) == 0 {
		return true
	}
	for _, remaining := range groups {
		if remaining > 0 {
			return false
		}
	}
	return true
}

func (t *Task) consumeDependency(target, group string) {
	if target == "" {
		return
	}
	if group == "" {
		group = target
	}
	groups, ok := t.remainingDeps[target]
	if !ok {
		return
	}
	if count, ok := groups[group]; ok && count > 0 {
		groups[group] = count - 1
	}
}

func cloneEdges(edges []conditionalEdge) []conditionalEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]conditionalEdge, len(edges))
	copy(out, edges)
	return out
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
