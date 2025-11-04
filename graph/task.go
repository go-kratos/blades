package graph

import (
	"context"
	"fmt"
	"sync"
)

// Task coordinates a single execution of the graph using a ready-queue based scheduler.
// This implementation combines:
// - Autogen's clean ready-queue + dependency counting approach
// - Blades' automatic skip propagation for complex routing scenarios
type Task struct {
	executor *Executor

	wg sync.WaitGroup

	mu        sync.Mutex
	readyCond *sync.Cond

	// Ready queue: nodes that are ready to execute (all dependencies satisfied)
	ready []string
	// Remaining dependencies: target -> count of unsatisfied predecessors
	remaining map[string]int
	// Contributions: target -> parent -> state (for aggregation)
	contributions map[string]map[string]State
	// In-flight: nodes currently executing
	inFlight map[string]bool
	// Visited: nodes that have completed
	visited map[string]bool

	finished    bool
	finishState State
	err         error
}

func newTask(e *Executor) *Task {
	// Initialize remaining dependencies count for each node from precomputed nodeInfo
	remaining := make(map[string]int, len(e.graph.nodes))
	for nodeName, info := range e.nodeInfos {
		if info.depCount > 0 {
			remaining[nodeName] = info.depCount
		}
	}

	// Initialize ready queue with nodes that have no dependencies
	ready := make([]string, 0, 4)
	ready = append(ready, e.graph.entryPoint)

	task := &Task{
		executor:      e,
		ready:         ready,
		remaining:     remaining,
		contributions: make(map[string]map[string]State),
		inFlight:      make(map[string]bool, len(e.graph.nodes)),
		visited:       make(map[string]bool, len(e.graph.nodes)),
	}
	task.readyCond = sync.NewCond(&task.mu)
	return task
}

func (t *Task) run(ctx context.Context, initial State) (State, error) {
	// Add initial contribution to entry point
	t.addInitialContribution(initial)

	// Main scheduling loop
	for {
		// Check termination conditions
		if shouldStop, result := t.checkTermination(); shouldStop {
			return result.state, result.err
		}

		// Schedule next ready node
		if !t.scheduleNext(ctx) {
			// No ready nodes, wait for in-flight to complete
			continue
		}
	}
}

// terminationResult holds the result when execution terminates
type terminationResult struct {
	state State
	err   error
}

// addInitialContribution adds the initial state to the entry point
func (t *Task) addInitialContribution(initial State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addContributionLocked(t.executor.graph.entryPoint, "start", initial)
}

// checkTermination checks if execution should terminate and returns the result
func (t *Task) checkTermination() (bool, terminationResult) {
	t.mu.Lock()

	if t.err != nil {
		err := t.err
		t.mu.Unlock()
		t.wg.Wait()
		return true, terminationResult{err: err}
	}

	if t.finished {
		state := t.finishState.Clone()
		t.mu.Unlock()
		t.wg.Wait()
		return true, terminationResult{state: state}
	}

	t.mu.Unlock()
	return false, terminationResult{}
}

// scheduleNext attempts to schedule the next ready node for execution.
// Returns false if no nodes are ready (caller should wait).
func (t *Task) scheduleNext(ctx context.Context) bool {
	t.mu.Lock()

	for len(t.ready) == 0 {
		if t.err != nil || t.finished {
			t.mu.Unlock()
			return false
		}
		if len(t.inFlight) == 0 {
			t.mu.Unlock()
			t.fail(fmt.Errorf("graph: finish node not reachable: %s", t.executor.graph.finishPoint))
			return false
		}
		t.readyCond.Wait()
	}

	// Check if we have ready nodes
	node := t.ready[0]
	t.ready = t.ready[1:]

	// Skip if already visited
	if t.visited[node] {
		t.mu.Unlock()
		return true
	}

	// Build aggregated state and mark as in-flight
	state := t.buildAggregateLocked(node)
	t.inFlight[node] = true
	t.wg.Add(1)
	parallel := t.executor.graph.parallel
	t.mu.Unlock()

	// Execute node (async if parallel mode)
	t.executeAsync(ctx, node, state, parallel)
	return true
}

// executeAsync executes a node either in a goroutine (parallel) or directly (serial)
func (t *Task) executeAsync(ctx context.Context, node string, state State, parallel bool) {
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
	// Check early termination
	t.mu.Lock()
	if t.err != nil || t.finished {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Execute handler
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

	// Mark as visited and get precomputed node info
	t.mu.Lock()
	t.visited[node] = true
	info := t.executor.nodeInfos[node]
	if info.isFinish && !t.finished {
		t.finished = true
		t.finishState = nextState.Clone()
		if t.readyCond != nil {
			t.readyCond.Broadcast()
		}
	}
	edges := info.outEdges
	t.mu.Unlock()

	// If this is the finish node, we're done (no outgoing edges guaranteed by compile-time validation)
	if info.isFinish {
		return
	}

	// Process outgoing edges (at least one edge guaranteed by compile-time validation)
	t.processOutgoing(ctx, node, edges, nextState)
}

func (t *Task) processOutgoing(ctx context.Context, node string, edges []conditionalEdge, state State) {
	// Evaluate edges: each edge is independent
	matched, skipped := t.evaluateEdges(ctx, edges, state)

	// Validate: at least one edge must match
	if len(matched) == 0 {
		t.fail(fmt.Errorf("graph: no condition matched for edges from node %s", node))
		return
	}

	// Register skips (this will trigger skip propagation)
	for _, edge := range skipped {
		t.satisfy(node, edge.to, nil)
	}

	// Propagate state along matched edges
	for _, edge := range matched {
		t.satisfy(node, edge.to, state.Clone())
	}
}

// evaluateEdges evaluates all edges and returns matched and skipped edges.
// Each edge is evaluated independently based on its condition.
func (t *Task) evaluateEdges(ctx context.Context, edges []conditionalEdge, state State) (matched, skipped []conditionalEdge) {
	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			matched = append(matched, edge)
		} else {
			skipped = append(skipped, edge)
		}
	}
	return matched, skipped
}

// satisfy handles both state propagation and skip registration in a unified way.
// When state is non-nil, it's a propagation (contribution); when nil, it's a skip.
func (t *Task) satisfy(from, to string, state State) {
	t.mu.Lock()

	// Early exit if already visited
	if t.visited[to] {
		t.mu.Unlock()
		return
	}

	info := t.executor.nodeInfos[to]
	if info.depCount == 0 {
		// No predecessors, nothing to track
		t.mu.Unlock()
		return
	}

	// Add contribution if state provided
	if state != nil {
		t.addContributionLocked(to, from, state)
	}

	// Decrement remaining count
	if t.remaining[to] > 0 {
		t.remaining[to]--
	}

	// Check if node is ready
	if t.remaining[to] == 0 && !t.visited[to] && !t.inFlight[to] {
		hasContributions := len(t.contributions[to]) > 0
		if !hasContributions {
			// All predecessors skipped - mark as skipped and propagate skip
			t.visited[to] = true
			edges := info.outEdges
			if t.readyCond != nil {
				t.readyCond.Signal()
			}
			t.mu.Unlock()
			// Propagate skip to all children
			for _, edge := range edges {
				t.satisfy(to, edge.to, nil)
			}
			return
		}
		// Has contributions - schedule for execution
		t.ready = append(t.ready, to)
		if t.readyCond != nil {
			t.readyCond.Signal()
		}
	}
	t.mu.Unlock()
}

func (t *Task) nodeDone(node string) {
	t.mu.Lock()
	delete(t.inFlight, node)
	if t.readyCond != nil {
		t.readyCond.Broadcast()
	}
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
	if t.readyCond != nil {
		t.readyCond.Broadcast()
	}
}

func (t *Task) buildAggregateLocked(node string) State {
	state := State{}
	contribs, ok := t.contributions[node]
	if !ok || len(contribs) == 0 {
		return state
	}

	// Use precomputed predecessors order from nodeInfo
	info := t.executor.nodeInfos[node]
	order := info.predecessors

	// Merge in predecessor order for determinism
	for _, parent := range order {
		if contribution, exists := contribs[parent]; exists {
			state = mergeStates(state, contribution)
			delete(contribs, parent)
		}
	}

	// Merge any remaining contributions (e.g., initial state for entry point)
	// Entry point has no predecessors but receives initial contribution from "start"
	for _, contribution := range contribs {
		state = mergeStates(state, contribution)
	}

	// Clean up contributions
	delete(t.contributions, node)
	return state
}

func (t *Task) addContributionLocked(node, parent string, state State) {
	if t.contributions[node] == nil {
		t.contributions[node] = make(map[string]State)
	}
	if _, exists := t.contributions[node][parent]; exists {
		// Ignore duplicate contribution
		return
	}
	t.contributions[node][parent] = state
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
