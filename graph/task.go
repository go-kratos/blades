package graph

import (
	"context"
	"fmt"
	"sync"
)

// Task coordinates a single execution of the graph.
type Task struct {
    executor *Executor

    wg sync.WaitGroup

	mu            sync.Mutex
	contributions map[string]map[string]State
	inFlight      map[string]bool
	visited       map[string]bool
	skippedFrom   map[string]map[string]bool

	finished    bool
	finishState State
	err         error
}

func newTask(e *Executor) *Task {
	return &Task{
		executor:      e,
		contributions: make(map[string]map[string]State),
		inFlight:      make(map[string]bool, len(e.graph.nodes)),
		visited:       make(map[string]bool, len(e.graph.nodes)),
		skippedFrom:   make(map[string]map[string]bool, len(e.graph.nodes)),
	}
}

func (t *Task) run(ctx context.Context, initial State) (State, error) {
    t.mu.Lock()
    t.addContributionLocked(t.executor.graph.entryPoint, "start", initial)
    t.mu.Unlock()
    t.trySchedule(ctx, t.executor.graph.entryPoint)

	t.wg.Wait()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.err != nil {
		return nil, t.err
	}
	if !t.finished {
		return nil, fmt.Errorf("graph: finish node not reachable: %s", t.executor.graph.finishPoint)
	}
	return t.finishState.Clone(), nil
}

func (t *Task) trySchedule(ctx context.Context, node string) {
    t.mu.Lock()
    // Short-circuit if task context is done, already failed, or finish reached
    if t.err != nil || t.finished {
        t.mu.Unlock()
        return
    }
	// Skip if node already handled or currently executing
	if t.visited[node] || t.inFlight[node] {
		t.mu.Unlock()
		return
	}
	// Proceed only when node is ready (all predecessors have either contributed or skipped)
	if !t.readyLocked(node) {
		t.mu.Unlock()
		return
	}

	state := t.buildAggregateLocked(node)
	t.inFlight[node] = true
	t.wg.Add(1)
	parallel := t.executor.graph.parallel
    t.mu.Unlock()

    run := func() {
        defer t.nodeDone(node)
        t.executeNode(ctx, node, state)
    }

	if parallel {
		go run()
	} else {
		run()
	}
}

func (t *Task) executeNode(ctx context.Context, node string, state State) {
    t.mu.Lock()
    if t.err != nil || t.finished {
        t.mu.Unlock()
        return
    }
    t.mu.Unlock()

	handler := t.executor.graph.nodes[node]

	if len(t.executor.graph.middlewares) > 0 {
		handler = ChainMiddlewares(t.executor.graph.middlewares...)(handler)
	}

    nodeCtx := NewNodeContext(ctx, &NodeContext{Name: node})
    nextState, err := handler(nodeCtx, state)
	if err != nil {
		t.fail(fmt.Errorf("graph: failed to execute node %s: %w", node, err))
		return
	}

	nextState = nextState.Clone()

	if t.markVisited(node, nextState) {
		return
	}

    t.processOutgoing(ctx, node, nextState)
}

func (t *Task) markVisited(node string, nextState State) bool {
	t.mu.Lock()
	t.visited[node] = true
	isFinish := node == t.executor.graph.finishPoint
	if isFinish && !t.finished {
		t.finished = true
		t.finishState = nextState.Clone()
	}
	finished := t.finished
	t.mu.Unlock()

    return finished && isFinish
}

func (t *Task) processOutgoing(ctx context.Context, node string, state State) {
    edges := t.executor.graph.edges[node]
    if len(edges) == 0 {
        t.fail(fmt.Errorf("graph: no outgoing edges from node %s", node))
        return
    }

    matched, skipped, err := resolveEdgeSelection(ctx, node, edges, state)
    if err != nil {
        t.fail(err)
        return
    }

    for _, edge := range skipped {
        t.registerSkip(ctx, node, edge)
    }

	for _, edge := range matched {
        ready := t.consumeAndAggregate(node, edge.to, state.Clone())
        if ready {
            t.trySchedule(ctx, edge.to)
        }
    }
}

func (t *Task) consumeAndAggregate(parent, target string, contribution State) bool {
	t.mu.Lock()
	t.addContributionLocked(target, parent, contribution)
	// Node is ready when all predecessors have either contributed or been marked as skipped
	ready := t.readyLocked(target) && !t.visited[target]
	t.mu.Unlock()
	return ready
}

func (t *Task) registerSkip(ctx context.Context, parent string, edge conditionalEdge) {
    target := edge.to

	t.mu.Lock()
	if t.visited[target] {
		t.mu.Unlock()
		return
	}

	preds := t.executor.predecessors[target]
	if len(preds) == 0 { // No predecessors to wait on
		t.mu.Unlock()
		return
	}
	if t.skippedFrom[target] == nil {
		t.skippedFrom[target] = make(map[string]bool)
	}
	if t.skippedFrom[target][parent] { // already marked skipped from this parent
		t.mu.Unlock()
		return
	}
	t.skippedFrom[target][parent] = true

	// Readiness and state presence snapshot
	seen := len(t.skippedFrom[target]) + len(t.contributions[target])
	total := len(preds)
	hasState := len(t.contributions[target]) > 0
	t.mu.Unlock()

	if seen < total {
		return
	}
    if !hasState { // all predecessors skipped, no contributions -> skip this node entirely
        t.markNodeSkipped(ctx, target)
        return
    }
    t.trySchedule(ctx, target)
}

func (t *Task) markNodeSkipped(ctx context.Context, node string) {
    t.mu.Lock()
    if t.visited[node] {
        t.mu.Unlock()
        return
    }
    t.visited[node] = true
    edges := cloneEdges(t.executor.graph.edges[node])
    t.mu.Unlock()

    for _, edge := range edges {
        t.registerSkip(ctx, node, edge)
    }
}

func (t *Task) nodeDone(node string) {
	t.mu.Lock()
	delete(t.inFlight, node)
	t.mu.Unlock()
	t.wg.Done()
}

func (t *Task) fail(err error) {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.err != nil {
        return
    }
    t.err = err
}

func (t *Task) buildAggregateLocked(node string) State {
	state := State{}
	if contribs, ok := t.contributions[node]; ok {
		order := t.executor.predecessors[node]
		for _, parent := range order {
			if contribution, exists := contribs[parent]; exists {
				state = mergeStates(state, contribution)
				delete(contribs, parent)
			}
		}
		for parent, contribution := range contribs {
			state = mergeStates(state, contribution)
			delete(contribs, parent)
		}
		delete(t.contributions, node)
	}
	return state
}

// readyLocked reports whether a node has received all required inputs
// (i.e., each predecessor either contributed or explicitly skipped).
// It must be called with t.mu held.
func (t *Task) readyLocked(node string) bool {
	preds := t.executor.predecessors[node]
	if len(preds) == 0 { // Source nodes
		return true
	}
	seen := len(t.skippedFrom[node]) + len(t.contributions[node])
	return seen >= len(preds)
}

// addContributionLocked adds a contribution for a node from a parent.
// If a contribution from the same parent already exists, it will not be added again,
// and a warning will be printed. This prevents unexpected state accumulation from duplicate edges.
func (t *Task) addContributionLocked(node, parent string, state State) {
	if t.contributions[node] == nil {
		t.contributions[node] = make(map[string]State)
	}
	if _, exists := t.contributions[node][parent]; exists {
		// Ignore duplicate contribution from same parent to keep state deterministic
		return
	}
	t.contributions[node][parent] = state
}

func resolveEdgeSelection(ctx context.Context, node string, edges []conditionalEdge, state State) ([]conditionalEdge, []conditionalEdge, error) {
	if len(edges) == 0 {
		return nil, nil, fmt.Errorf("graph: no outgoing edges from node %s", node)
	}

	// Fast-path: all unconditional
	allUnconditional := true
	for _, e := range edges {
		if e.condition != nil {
			allUnconditional = false
			break
		}
	}
	if allUnconditional {
		return cloneEdges(edges), nil, nil
	}

	// Collect leading unconditional edges
	leading := 0
	for leading < len(edges) && edges[leading].condition == nil {
		leading++
	}
	matched := cloneEdges(edges[:leading])
	if leading == len(edges) {
		return matched, nil, nil
	}

	// Evaluate remaining edges: conditional(s) and possible unconditional fallback
	var (
		rest        = edges[leading:]
		restMatched []conditionalEdge
		skipped     []conditionalEdge
		hasFallback bool
	)

	for i, e := range rest {
		if e.condition == nil { // unconditional fallback
			restMatched = append(restMatched, e)
			hasFallback = true
			// Everything after fallback is skipped
			if i+1 < len(rest) {
				skipped = append(skipped, cloneEdges(rest[i+1:])...)
			}
			break
		}
		if e.condition(ctx, state) {
			restMatched = append(restMatched, e)
			// If a fallback exists later, prefer first match then fallback behavior
			if i+1 < len(rest) {
				for _, tr := range rest[i+1:] {
					if tr.condition == nil {
						hasFallback = true
						skipped = append(skipped, cloneEdges(rest[i+1:])...)
						break
					}
				}
				if hasFallback {
					break
				}
			}
		} else {
			skipped = append(skipped, e)
		}
	}

	if len(matched)+len(restMatched) == 0 {
		return nil, nil, fmt.Errorf("graph: no condition matched for edges from node %s", node)
	}
	// When an unconditional fallback follows conditionals, only the first match is used.
	if hasFallback && len(restMatched) > 1 {
		restMatched = restMatched[:1]
	}
	matched = append(matched, restMatched...)
	return matched, skipped, nil
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
