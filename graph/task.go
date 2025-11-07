package graph

import (
	"context"
	"fmt"
	"sync"
)

const entryContributionParent = "graph_entry"

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
	// Skips: target -> parent -> true (tracks which parents sent skip)
	skips map[string]map[string]bool
	// Pending re-execution for loop edges while node still in-flight
	pendingRuns map[string]bool
	// Pending skip propagation when deferred for loops
	pendingSkips map[string]bool
	// Tracks nodes that were skipped (so they can be reactivated later)
	skipped map[string]bool
	// Tracks nodes currently executing as part of a loop iteration
	looping map[string]bool
	// Number of contributions observed per node
	received map[string]int
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
		if info.dependencies > 0 {
			remaining[nodeName] = info.dependencies
		}
	}
	task := &Task{
		executor:      e,
		ready:         make([]string, 0, 4),
		remaining:     remaining,
		contributions: make(map[string]map[string]State),
		skips:         make(map[string]map[string]bool),
		pendingRuns:   make(map[string]bool),
		pendingSkips:  make(map[string]bool),
		skipped:       make(map[string]bool),
		looping:       make(map[string]bool),
		received:      make(map[string]int),
		inFlight:      make(map[string]bool, len(e.graph.nodes)),
		visited:       make(map[string]bool, len(e.graph.nodes)),
	}
	task.readyCond = sync.NewCond(&task.mu)
	return task
}

func (t *Task) run(ctx context.Context, state State) (State, error) {
	// Add initial contribution to entry point
	t.addInitialContribution(state)
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
	if t.addContributionLocked(t.executor.graph.entryPoint, entryContributionParent, initial) {
		t.received[t.executor.graph.entryPoint]++
	}
	t.ready = append(t.ready, t.executor.graph.entryPoint)
}

// checkTermination checks if execution should terminate and returns the result
func (t *Task) checkTermination() (bool, terminationResult) {
	t.mu.Lock()
	err := t.err
	finished := t.finished
	state := t.finishState
	t.mu.Unlock()

	if err != nil {
		t.wg.Wait()
		return true, terminationResult{err: err}
	}

	if finished {
		t.wg.Wait()
		return true, terminationResult{state: state}
	}

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

	// Mark as visited and get precomputed node info
	t.mu.Lock()
	t.visited[node] = true
	info := t.executor.nodeInfos[node]
	if info.isFinish && !t.finished {
		t.finished = true
		t.finishState = nextState
		t.readyCond.Broadcast()
	}
	t.mu.Unlock()

	// If this is the finish node, we're done (no outgoing edges guaranteed by compile-time validation)
	if info.isFinish {
		return
	}

	// Process outgoing edges (at least one edge guaranteed by compile-time validation)
	t.processOutgoing(ctx, node, info, nextState)
}

func (t *Task) processOutgoing(ctx context.Context, node string, info *nodeInfo, state State) {
	if !info.hasConditions {
		for _, dest := range info.unconditionalDests {
			t.satisfy(node, dest, state.Clone())
		}
		return
	}

	// Exclusive matching: only the first matching edge is activated
	var matchedEdge *conditionalEdge
	for i := range info.outEdges {
		edge := &info.outEdges[i]
		if edge.condition == nil {
			t.fail(fmt.Errorf("graph: conditional edge from node %s to %s missing condition", node, edge.to))
			return
		}
		if edge.condition(ctx, state) {
			matchedEdge = edge
			break
		}
	}

	if matchedEdge == nil {
		t.fail(fmt.Errorf("graph: no condition matched for edges from node %s", node))
		return
	}

	// Activate the matched edge
	// Clone to ensure state isolation: prevents shared map mutations
	// and converts nil to empty State{} (backward compatible)
	t.satisfy(node, matchedEdge.to, state.Clone())

	// Send skip to all other edges
	for i := range info.outEdges {
		edge := &info.outEdges[i]
		if edge.to != matchedEdge.to {
			t.satisfy(node, edge.to, nil)
		}
	}
}

// satisfy handles both state propagation and skip registration in a unified way.
// When state is non-nil, it's a propagation (contribution); when nil, it's a skip.
// For Loop edges, allows revisiting already-visited nodes.
func (t *Task) satisfy(from, to string, state State) {
	var (
		propagateSkip bool
		skipEdges     []conditionalEdge
	)

	t.mu.Lock()

	toInfo := t.executor.nodeInfos[to]
	if toInfo == nil {
		t.mu.Unlock()
		return
	}

	isLoopEdge := toInfo.loopEdgeSources != nil && toInfo.loopEdgeSources[from]
	deferSchedule, stop := t.prepareNodeLocked(from, to, state, toInfo, isLoopEdge)
	if stop {
		t.mu.Unlock()
		return
	}

	if toInfo.dependencies == 0 && !isLoopEdge {
		t.mu.Unlock()
		return
	}

	if !t.registerSignalLocked(to, from, state) {
		t.mu.Unlock()
		return
	}

	if t.remaining[to] > 0 {
		t.remaining[to]--
	}

	if t.remaining[to] == 0 && !t.visited[to] {
		var handled bool
		propagateSkip, skipEdges, handled = t.handleReadyLocked(to, toInfo, deferSchedule)
		if handled {
			t.mu.Unlock()
			if propagateSkip {
				for _, edge := range skipEdges {
					t.satisfy(to, edge.to, nil)
				}
			}
			return
		}
	}

	t.mu.Unlock()
}

func (t *Task) nodeDone(node string) {
	var (
		propagateSkip bool
		skipEdges     []conditionalEdge
	)

	t.mu.Lock()
	delete(t.inFlight, node)
	delete(t.looping, node)

	switch {
	case t.pendingSkips[node]:
		delete(t.pendingSkips, node)
		if !t.visited[node] {
			t.visited[node] = true
		}
		t.skipped[node] = true
		delete(t.received, node)
		delete(t.contributions, node)
		delete(t.skips, node)
		if info := t.executor.nodeInfos[node]; info != nil {
			skipEdges = collectNonLoopEdges(info.outEdges)
		}
		propagateSkip = true
	case t.pendingRuns[node]:
		delete(t.pendingRuns, node)
		if t.received[node] != 0 && !t.visited[node] {
			delete(t.skipped, node)
			t.ready = append(t.ready, node)
			t.readyCond.Signal()
		}
	}

	t.readyCond.Broadcast()
	t.mu.Unlock()

	if propagateSkip {
		for _, edge := range skipEdges {
			t.satisfy(node, edge.to, nil)
		}
	}

	t.wg.Done()
}

func (t *Task) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return
	}
	t.err = err
	t.readyCond.Broadcast()
}

func (t *Task) buildAggregateLocked(node string) State {
	state := State{}
	contribs, ok := t.contributions[node]
	if !ok || len(contribs) == 0 {
		delete(t.received, node)
		return state
	}

	// Use precomputed predecessors order from nodeInfo
	info := t.executor.nodeInfos[node]
	order := info.predecessors

	// Merge in predecessor order for determinism; the entry node's list already includes the synthetic parent
	for _, parent := range order {
		if contribution, exists := contribs[parent]; exists {
			state = mergeStates(state, contribution)
		}
	}

	// Clean up contributions
	delete(t.contributions, node)
	delete(t.received, node)
	return state
}

func (t *Task) addContributionLocked(node, parent string, state State) bool {
	if t.contributions[node] == nil {
		t.contributions[node] = make(map[string]State)
	}
	if _, exists := t.contributions[node][parent]; exists {
		// Ignore duplicate contribution
		return false
	}
	t.contributions[node][parent] = state
	return true
}

func (t *Task) addSkipLocked(node, parent string) bool {
	if t.skips[node] == nil {
		t.skips[node] = make(map[string]bool)
	}
	if t.skips[node][parent] {
		// Duplicate skip
		return false
	}
	t.skips[node][parent] = true
	return true
}

func (t *Task) prepareNodeLocked(from, to string, state State, info *nodeInfo, isLoopEdge bool) (bool, bool) {
	if isLoopEdge {
		return t.prepareLoopNodeLocked(to, state, info), false
	}
	return t.prepareNormalNodeLocked(from, to, state, info)
}

func (t *Task) prepareLoopNodeLocked(to string, state State, info *nodeInfo) bool {
	deferSchedule := false
	if t.inFlight[to] {
		deferSchedule = true
	}
	if t.visited[to] {
		loopDeps := info.loopDependencies
		if loopDeps <= 0 {
			loopDeps = 1
		}
		t.resetNodeStateLocked(to, loopDeps)
	}
	if state != nil {
		t.looping[to] = true
	}
	return deferSchedule
}

func (t *Task) prepareNormalNodeLocked(from, to string, state State, info *nodeInfo) (bool, bool) {
	if t.visited[to] {
		if state != nil && (t.skipped[to] || t.looping[from]) {
			t.resetNodeStateLocked(to, info.dependencies)
			delete(t.skipped, to)
			if t.looping[from] {
				t.looping[to] = true
			}
			return false, false
		}
		return false, true
	}
	if state != nil && t.looping[from] {
		t.looping[to] = true
	}
	return false, false
}

func (t *Task) registerSignalLocked(node, parent string, state State) bool {
	if state != nil {
		if !t.addContributionLocked(node, parent, state) {
			return false
		}
		t.received[node]++
		delete(t.skipped, node)
		return true
	}
	t.skipped[node] = true
	return t.addSkipLocked(node, parent)
}

func (t *Task) handleReadyLocked(node string, info *nodeInfo, deferSchedule bool) (bool, []conditionalEdge, bool) {
	if t.received[node] == 0 {
		if deferSchedule {
			t.pendingSkips[node] = true
			return false, nil, true
		}
		t.visited[node] = true
		delete(t.received, node)
		t.readyCond.Signal()
		return true, collectNonLoopEdges(info.outEdges), true
	}

	if deferSchedule {
		delete(t.skipped, node)
		t.pendingRuns[node] = true
		return false, nil, true
	}

	if t.inFlight[node] {
		return false, nil, true
	}

	delete(t.skipped, node)
	t.ready = append(t.ready, node)
	t.readyCond.Signal()
	return false, nil, true
}

func (t *Task) resetNodeStateLocked(node string, remaining int) {
	delete(t.visited, node)
	delete(t.contributions, node)
	delete(t.skips, node)
	t.received[node] = 0
	t.remaining[node] = remaining
}

func collectNonLoopEdges(edges []conditionalEdge) []conditionalEdge {
	if len(edges) == 0 {
		return nil
	}
	out := make([]conditionalEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.edgeType == EdgeTypeLoop {
			continue
		}
		out = append(out, edge)
	}
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
