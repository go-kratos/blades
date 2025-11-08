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
	// Runtime state per node (remaining deps, visit flags, etc.)
	nodes map[string]*nodeRuntime
	// Count of nodes currently in flight
	inFlightCount int

	finished    bool
	finishState State
	err         error
}

type edgePrepAction int

const (
	prepContinue edgePrepAction = iota
	prepDefer
	prepStop
)

func newTask(e *Executor) *Task {
	nodes := make(map[string]*nodeRuntime, len(e.graph.nodes))
	for nodeName, info := range e.nodeInfos {
		state := &nodeRuntime{
			remaining: info.dependencies,
			info:      info,
		}
		size := len(info.predecessors)
		if size > 0 {
			state.contributions = make([]State, size)
			state.skipMarks = make([]bool, size)
		}
		nodes[nodeName] = state
	}
	task := &Task{
		executor: e,
		ready:    make([]string, 0, 4),
		nodes:    nodes,
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
	entry := t.executor.graph.entryPoint
	if t.addContributionLocked(entry, entryContributionParent, initial) {
		t.nodeState(entry).received++
	}
	t.enqueueReadyLocked(entry)
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
		// No ready work and nothing in-flight means we're stuck before reaching finish.
		if t.inFlightCount == 0 {
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
	if t.nodeState(node).visited {
		t.mu.Unlock()
		return true
	}

	// Build aggregated state and mark as in-flight
	state := t.buildAggregateLocked(node)
	nodeState := t.nodeState(node)
	nodeState.inFlight = true
	t.inFlightCount++
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
	nodeState := t.nodeState(node)
	nodeState.visited = true
	info := nodeState.info
	if info.isFinish && !t.finished {
		t.finished = true
		t.finishState = nextState
		t.readyCond.Broadcast()
	}
	t.mu.Unlock()

	// Finish nodes may declare edges, but we short-circuit after recording the result.
	if info.isFinish {
		return
	}

	// Process outgoing edges (at least one edge guaranteed by compile-time validation)
	t.processOutgoing(ctx, node, info, nextState)
}

func (t *Task) processOutgoing(ctx context.Context, node string, info *nodeInfo, state State) {
	if !info.hasConditions {
		for i := range info.outEdges {
			edge := &info.outEdges[i]
			t.satisfy(node, edge, state.Clone())
		}
		return
	}

	// Exclusive matching: only the first matching edge is activated
	var matchedEdge *conditionalEdge
	for i := range info.outEdges {
		edge := &info.outEdges[i]
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
	t.satisfy(node, matchedEdge, state.Clone())

	// Send skip to all other edges
	for i := range info.outEdges {
		edge := &info.outEdges[i]
		if edge == matchedEdge {
			continue
		}
		t.satisfy(node, edge, nil)
	}
}

// satisfy handles both state propagation and skip registration in a unified way.
// When state is non-nil, it's a propagation (contribution); when nil, it's a skip.
// For Loop edges, allows revisiting already-visited nodes.
func (t *Task) satisfy(from string, edge *conditionalEdge, state State) {
	var (
		propagateSkip bool
		skipEdges     []conditionalEdge
	)

	t.mu.Lock()

	to := edge.to
	toInfo := t.executor.nodeInfos[to]
	if toInfo == nil {
		t.mu.Unlock()
		return
	}

	isLoopEdge := edge.edgeType == EdgeTypeLoop
	action := t.prepareNodeLocked(from, to, state, toInfo, isLoopEdge)
	if action == prepStop {
		t.mu.Unlock()
		return
	}
	deferSchedule := action == prepDefer

	if !t.registerSignalLocked(to, from, state) {
		t.mu.Unlock()
		return
	}

	toState := t.nodeState(to)
	if toState.remaining > 0 {
		toState.remaining--
	}

	if toState.remaining == 0 && !toState.visited {
		propagateSkip, skipEdges = t.handleReadyLocked(to, toInfo, deferSchedule)
		t.mu.Unlock()
		if propagateSkip {
			t.propagateSkipEdges(to, skipEdges)
		}
		return
	}

	t.mu.Unlock()
}

func (t *Task) nodeDone(node string) {
	var (
		propagateSkip bool
		skipEdges     []conditionalEdge
	)

	t.mu.Lock()
	state := t.nodeState(node)
	if state.inFlight {
		state.inFlight = false
		if t.inFlightCount > 0 {
			t.inFlightCount--
		}
	}
	state.looping = false

	switch {
	case state.pendingSkip:
		state.pendingSkip = false
		if !state.visited {
			state.visited = true
		}
		state.skipped = true
		state.received = 0
		clearStateSlice(state.contributions)
		clearBoolSlice(state.skipMarks)
		if info := state.info; info != nil {
			skipEdges = info.nonLoopEdges
		}
		propagateSkip = true
	case state.pendingRun:
		state.pendingRun = false
		if state.received != 0 && !state.visited {
			state.skipped = false
			t.enqueueReadyLocked(node)
		}
	}

	t.readyCond.Broadcast()
	t.mu.Unlock()

	if propagateSkip {
		t.propagateSkipEdges(node, skipEdges)
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
	nodeState := t.nodeState(node)
	if nodeState.received == 0 {
		return state
	}
	contribs := nodeState.contributions

	// Use precomputed predecessors order from nodeInfo
	info := nodeState.info
	for idx := range info.predecessors {
		if idx >= len(contribs) {
			break
		}
		if contribution := contribs[idx]; contribution != nil {
			state = mergeStates(state, contribution)
		}
	}

	// Clean up contributions
	clearStateSlice(contribs)
	nodeState.received = 0
	return state
}

func (t *Task) addContributionLocked(node, parent string, state State) bool {
	nodeState := t.nodeState(node)
	info := nodeState.info
	if info == nil {
		return false
	}
	idx, ok := info.parentIndex[parent]
	if !ok {
		return false
	}
	if nodeState.contributions == nil {
		nodeState.contributions = make([]State, len(info.predecessors))
	}
	if nodeState.contributions[idx] != nil {
		return false
	}
	nodeState.contributions[idx] = state
	return true
}

func (t *Task) addSkipLocked(node, parent string) bool {
	nodeState := t.nodeState(node)
	info := nodeState.info
	if info == nil {
		return false
	}
	idx, ok := info.parentIndex[parent]
	if !ok {
		return false
	}
	if nodeState.skipMarks == nil {
		nodeState.skipMarks = make([]bool, len(info.predecessors))
	}
	if nodeState.skipMarks[idx] {
		return false
	}
	nodeState.skipMarks[idx] = true
	return true
}

func (t *Task) prepareNodeLocked(from, to string, state State, info *nodeInfo, isLoopEdge bool) edgePrepAction {
	if isLoopEdge {
		return t.prepareLoopNodeLocked(to, state, info)
	}
	return t.prepareNormalNodeLocked(from, to, state, info)
}

func (t *Task) prepareLoopNodeLocked(to string, state State, info *nodeInfo) edgePrepAction {
	deferSchedule := false
	toState := t.nodeState(to)
	if toState.inFlight {
		deferSchedule = true
	}
	if toState.visited {
		loopDeps := info.loopDependencies
		if loopDeps <= 0 {
			loopDeps = 1
		}
		t.resetNodeStateLocked(to, loopDeps)
	}
	if state != nil {
		toState.looping = true
	}
	if deferSchedule {
		return prepDefer
	}
	return prepContinue
}

func (t *Task) prepareNormalNodeLocked(from, to string, state State, info *nodeInfo) edgePrepAction {
	toState := t.nodeState(to)
	fromState := t.nodeState(from)
	if toState.visited {
		if state != nil && (toState.skipped || fromState.looping) {
			t.resetNodeStateLocked(to, info.dependencies)
			toState.skipped = false
			if fromState.looping {
				toState.looping = true
			}
			return prepContinue
		}
		return prepStop
	}
	if state != nil && fromState.looping {
		toState.looping = true
	}
	return prepContinue
}

func (t *Task) registerSignalLocked(node, parent string, state State) bool {
	nodeState := t.nodeState(node)
	if state != nil {
		if !t.addContributionLocked(node, parent, state) {
			return false
		}
		nodeState.received++
		nodeState.skipped = false
		return true
	}
	nodeState.skipped = true
	return t.addSkipLocked(node, parent)
}

func (t *Task) handleReadyLocked(node string, info *nodeInfo, deferSchedule bool) (bool, []conditionalEdge) {
	state := t.nodeState(node)
	if state.received == 0 {
		if deferSchedule {
			state.pendingSkip = true
			return false, nil
		}
		state.visited = true
		state.received = 0
		t.readyCond.Signal()
		return true, info.nonLoopEdges
	}

	if deferSchedule {
		state.skipped = false
		state.pendingRun = true
		return false, nil
	}

	if state.inFlight {
		return false, nil
	}

	state.skipped = false
	t.enqueueReadyLocked(node)
	return false, nil
}

func (t *Task) resetNodeStateLocked(node string, remaining int) {
	state := t.nodeState(node)
	state.visited = false
	state.inFlight = false
	state.skipped = false
	state.looping = false
	state.pendingRun = false
	state.pendingSkip = false
	state.received = 0
	state.remaining = remaining
	clearStateSlice(state.contributions)
	clearBoolSlice(state.skipMarks)
}

// nodeRuntime houses all mutable per-node scheduling state.
type nodeRuntime struct {
	remaining     int       // outstanding non-loop dependencies before the node becomes ready
	received      int       // number of contributions accumulated for this execution
	inFlight      bool      // node is currently executing
	visited       bool      // node has completed its current execution
	skipped       bool      // node was skipped in the current iteration
	looping       bool      // node participates in an active loop iteration
	pendingRun    bool      // node should be re-enqueued once current execution finishes
	pendingSkip   bool      // node should propagate skips once current execution finishes
	contributions []State   // ordered contributions indexed by parentIndex
	skipMarks     []bool    // per-parent skip markers aligned with parentIndex
	info          *nodeInfo // immutable metadata for the node
}

func (t *Task) nodeState(name string) *nodeRuntime {
	if state, ok := t.nodes[name]; ok {
		return state
	}
	state := &nodeRuntime{
		info: t.executor.nodeInfos[name],
	}
	if info := state.info; info != nil {
		size := len(info.predecessors)
		if size > 0 {
			state.contributions = make([]State, size)
			state.skipMarks = make([]bool, size)
		}
	}
	t.nodes[name] = state
	return state
}

func (t *Task) enqueueReadyLocked(node string) {
	t.ready = append(t.ready, node)
	t.readyCond.Signal()
}

func (t *Task) propagateSkipEdges(from string, edges []conditionalEdge) {
	for i := range edges {
		edge := &edges[i]
		t.satisfy(from, edge, nil)
	}
}

func clearStateSlice(states []State) {
	for i := range states {
		states[i] = nil
	}
}

func clearBoolSlice(flags []bool) {
	for i := range flags {
		flags[i] = false
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
