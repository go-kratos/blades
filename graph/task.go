package graph

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// Task coordinates a single execution of the graph using a ready-queue based scheduler.
// This implementation combines:
// - Autogen's clean ready-queue + dependency counting approach
// - Blades' automatic skip propagation for complex routing scenarios
type Task struct {
	executor *Executor
	state    State

	wg sync.WaitGroup

	mu        sync.Mutex
	readyCond *sync.Cond

	// Ready queue: nodes that are ready to execute (all dependencies satisfied)
	ready []string
	// Remaining dependencies: target -> count of unsatisfied predecessors
	remaining map[string]int
	// Number of contributions observed per node
	received map[string]int
	// In-flight: nodes currently executing
	inFlight map[string]bool
	// Visited: nodes that have completed
	visited map[string]bool

	checkpointer            Checkpointer
	taskID                  string
	progressSinceCheckpoint bool

	finished bool
	err      error
}

func newTask(e *Executor, state State, checkpointer Checkpointer, taskID string) *Task {
	// Initialize remaining dependencies count for each node from precomputed nodeInfo
	state.ensure()
	task := &Task{
		executor:     e,
		state:        state,
		ready:        make([]string, 0, 4),
		remaining:    make(map[string]int, len(e.graph.nodes)),
		received:     make(map[string]int),
		inFlight:     make(map[string]bool, len(e.graph.nodes)),
		visited:      make(map[string]bool, len(e.graph.nodes)),
		checkpointer: checkpointer,
		taskID:       taskID,
	}
	task.readyCond = sync.NewCond(&task.mu)
	return task
}

func (t *Task) run(ctx context.Context, checkpoint *Checkpoint) (State, error) {
	if checkpoint != nil {
		t.restoreCheckpoint(*checkpoint)
	} else {
		t.prepareEntry()
	}
	// Main scheduling loop
	for {
		t.emitCheckpointIfIdle()
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

// prepareEntry seeds the entry node as ready to run.
func (t *Task) prepareEntry() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Initialize remaining dependencies
	for nodeName, info := range t.executor.nodeInfos {
		if info.dependencies > 0 {
			t.remaining[nodeName] = info.dependencies
		}
	}
	t.received[t.executor.graph.entryPoint]++
	t.ready = append(t.ready, t.executor.graph.entryPoint)
}

func (t *Task) restoreCheckpoint(cp Checkpoint) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.received = maps.Clone(cp.Received)
	if t.received == nil {
		t.received = make(map[string]int)
	}

	t.visited = maps.Clone(cp.Visited)
	if t.visited == nil {
		t.visited = make(map[string]bool)
	}

	t.inFlight = make(map[string]bool, len(t.executor.graph.nodes))
	t.state = StateFromMap(cp.State)
	t.state.ensure()
	t.rebuildRemainingLocked()
	t.rebuildReadyLocked()
	t.finished = t.visited[t.executor.graph.finishPoint]
	t.err = nil
	t.progressSinceCheckpoint = false
}

func (t *Task) shouldCheckpointLocked() bool {
	return t.checkpointer != nil && t.taskID != "" && t.progressSinceCheckpoint && len(t.inFlight) == 0
}

// rebuildRemainingLocked derives remaining counts from visited nodes and graph topology.
func (t *Task) rebuildRemainingLocked() {
	t.remaining = make(map[string]int, len(t.executor.graph.nodes))
	satisfied := make(map[string]int, len(t.executor.graph.nodes))

	// Count satisfied predecessors based on visited nodes propagating to their children.
	for from, edges := range t.executor.graph.edges {
		if !t.visited[from] {
			continue
		}
		for _, edge := range edges {
			satisfied[edge.to]++
		}
	}

	for nodeName, info := range t.executor.nodeInfos {
		if info.dependencies == 0 {
			continue
		}
		remaining := info.dependencies - satisfied[nodeName]
		if remaining > 0 {
			t.remaining[nodeName] = remaining
		}
	}
}

// rebuildReadyLocked rebuilds the ready queue consistent with remaining/visited/received.
func (t *Task) rebuildReadyLocked() {
	t.ready = t.ready[:0]
	for nodeName, info := range t.executor.nodeInfos {
		if info.dependencies == 0 && !t.visited[nodeName] {
			// Dependency-free nodes are ready if they had any activation (entry is handled elsewhere)
			if t.received[nodeName] > 0 {
				t.ready = append(t.ready, nodeName)
			}
			continue
		}
		if t.visited[nodeName] || t.remaining[nodeName] > 0 {
			continue
		}
		if t.received[nodeName] == 0 {
			continue
		}
		t.ready = append(t.ready, nodeName)
	}
}

func (t *Task) emitCheckpointIfIdle() {
	if t.checkpointer == nil || t.taskID == "" {
		return
	}

	t.mu.Lock()
	if !t.shouldCheckpointLocked() {
		t.mu.Unlock()
		return
	}
	checkpoint := t.buildCheckpointLocked()
	t.progressSinceCheckpoint = false
	t.mu.Unlock()

	if err := t.checkpointer.Save(t.taskID, checkpoint); err != nil {
		t.fail(fmt.Errorf("graph: checkpoint save failed: %w", err))
	}
}

// checkTermination checks if execution should terminate and returns the result
func (t *Task) checkTermination() (bool, terminationResult) {
	t.mu.Lock()
	err := t.err
	finished := t.finished
	state := t.state
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

	if t.shouldCheckpointLocked() {
		t.mu.Unlock()
		return false
	}

	for len(t.ready) == 0 {
		if t.err != nil || t.finished {
			t.mu.Unlock()
			return false
		}
		if t.shouldCheckpointLocked() {
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

	if t.shouldCheckpointLocked() {
		t.mu.Unlock()
		return false
	}

	// Check if we have ready nodes
	node := t.ready[0]
	t.ready = t.ready[1:]

	// Skip if already visited
	if t.visited[node] {
		t.mu.Unlock()
		return true
	}

	// Mark as in-flight
	state := t.state
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
	err := handler(nodeCtx, state)
	if err != nil {
		t.fail(fmt.Errorf("graph: failed to execute node %s: %w", node, err))
		return
	}

	// Mark as visited and get precomputed node info
	t.mu.Lock()
	t.visited[node] = true
	t.progressSinceCheckpoint = true
	info := t.executor.nodeInfos[node]
	if info.isFinish && !t.finished {
		t.finished = true
		t.readyCond.Broadcast()
	}
	t.mu.Unlock()

	// If this is the finish node, we're done (no outgoing edges guaranteed by compile-time validation)
	if info.isFinish {
		return
	}

	// Process outgoing edges (at least one edge guaranteed by compile-time validation)
	t.processOutgoing(ctx, node, info, state)
}

func (t *Task) processOutgoing(ctx context.Context, node string, info *nodeInfo, state State) {
	if !info.hasConditions {
		for _, dest := range info.unconditionalDests {
			t.satisfy(node, dest, true)
		}
		return
	}

	matched := false
	for _, edge := range info.outEdges {
		if edge.condition == nil {
			t.fail(fmt.Errorf("graph: conditional edge from node %s to %s missing condition", node, edge.to))
			return
		}
		if edge.condition(ctx, state) {
			matched = true
			t.satisfy(node, edge.to, true)
		} else {
			t.satisfy(node, edge.to, false)
		}
	}

	if !matched {
		t.fail(fmt.Errorf("graph: no condition matched for edges from node %s", node))
		return
	}
}

// satisfy handles dependency fulfillment and skip registration.
// activated indicates whether the edge was taken (true) or skipped (false).
func (t *Task) satisfy(from, to string, activated bool) {
	t.mu.Lock()

	// Early exit if already visited
	if t.visited[to] {
		t.mu.Unlock()
		return
	}

	info := t.executor.nodeInfos[to]
	if info.dependencies == 0 {
		// No predecessors, nothing to track
		t.mu.Unlock()
		return
	}

	// Track active contributions
	if activated {
		t.received[to]++
	}

	// Decrement remaining count
	if t.remaining[to] > 0 {
		t.remaining[to]--
	}

	// Check if node is ready
	if t.remaining[to] == 0 && !t.visited[to] && !t.inFlight[to] {
		if t.received[to] == 0 {
			// All predecessors skipped - mark as skipped and propagate skip
			t.visited[to] = true
			t.progressSinceCheckpoint = true
			delete(t.received, to)
			t.readyCond.Signal()
			t.mu.Unlock()
			for _, edge := range info.outEdges {
				t.satisfy(to, edge.to, false)
			}
			return
		}
		// Has contributions - schedule for execution
		t.ready = append(t.ready, to)
		t.readyCond.Signal()
	}
	t.mu.Unlock()
}

func (t *Task) nodeDone(node string) {
	t.mu.Lock()
	delete(t.inFlight, node)
	t.readyCond.Broadcast()
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
	t.readyCond.Broadcast()
}

func (t *Task) buildCheckpointLocked() Checkpoint {
	checkpoint := Checkpoint{
		Received: maps.Clone(t.received),
		Visited:  maps.Clone(t.visited),
		State:    t.state.Snapshot(),
	}
	return checkpoint
}
