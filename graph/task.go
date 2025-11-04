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

	mu sync.Mutex
	// Ready queue: nodes that are ready to execute (all dependencies satisfied)
	ready []string
	// Remaining dependencies: target -> count of unsatisfied predecessors
	remaining map[string]int
	// Contributions: target -> parent -> state (for aggregation)
	contributions map[string]map[string]State
	// Skipped from: target -> set of parents that skipped it
	skippedFrom map[string]map[string]bool
	// In-flight: nodes currently executing
	inFlight map[string]bool
	// Visited: nodes that have completed
	visited map[string]bool

	finished    bool
	finishState State
	err         error
}

func newTask(e *Executor) *Task {
	// Initialize remaining dependencies count for each node
	remaining := make(map[string]int, len(e.graph.nodes))
	for target, count := range e.dependencies {
		remaining[target] = count
	}

	// Initialize ready queue with nodes that have no dependencies
	ready := make([]string, 0, 4)
	ready = append(ready, e.graph.entryPoint)

	return &Task{
		executor:      e,
		ready:         ready,
		remaining:     remaining,
		contributions: make(map[string]map[string]State),
		skippedFrom:   make(map[string]map[string]bool),
		inFlight:      make(map[string]bool, len(e.graph.nodes)),
		visited:       make(map[string]bool, len(e.graph.nodes)),
	}
}

func (t *Task) run(ctx context.Context, initial State) (State, error) {
	// Add initial contribution to entry point
	t.mu.Lock()
	t.addContributionLocked(t.executor.graph.entryPoint, "start", initial)
	t.mu.Unlock()

	// Main scheduling loop
	for {
		t.mu.Lock()
		// Check termination conditions
		if t.err != nil {
			err := t.err
			t.mu.Unlock()
			t.wg.Wait()
			return nil, err
		}
		if t.finished {
			state := t.finishState.Clone()
			t.mu.Unlock()
			t.wg.Wait()
			return state, nil
		}

		// Get next ready node
		if len(t.ready) == 0 {
			// No more ready nodes
			if len(t.inFlight) == 0 {
				// Nothing in flight either - graph is stuck or done
				if !t.finished {
					t.mu.Unlock()
					t.wg.Wait()
					return nil, fmt.Errorf("graph: finish node not reachable: %s", t.executor.graph.finishPoint)
				}
			}
			// Wait for in-flight nodes to complete
			t.mu.Unlock()
			t.wg.Wait()
			continue
		}

		// Dequeue next node
		node := t.ready[0]
		t.ready = t.ready[1:]

		// Skip if already visited
		if t.visited[node] {
			t.mu.Unlock()
			continue
		}

		// Build aggregated state from contributions
		state := t.buildAggregateLocked(node)
		t.inFlight[node] = true
		t.wg.Add(1)
		parallel := t.executor.graph.parallel
		t.mu.Unlock()

		// Execute node
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

	// Mark as visited
	t.mu.Lock()
	t.visited[node] = true
	isFinish := node == t.executor.graph.finishPoint
	if isFinish && !t.finished {
		t.finished = true
		t.finishState = nextState.Clone()
	}
	edges := cloneEdges(t.executor.graph.edges[node])
	t.mu.Unlock()

	// If this is the finish node, we're done
	if isFinish {
		return
	}

	// Process outgoing edges
	t.processOutgoing(ctx, node, edges, nextState)
}

func (t *Task) processOutgoing(ctx context.Context, node string, edges []conditionalEdge, state State) {
	if len(edges) == 0 {
		t.fail(fmt.Errorf("graph: no outgoing edges from node %s", node))
		return
	}

	// Simplified edge evaluation: each edge is independent
	matched, skipped := t.evaluateEdges(ctx, edges, state)

	// Validate: at least one edge must match
	if len(matched) == 0 {
		t.fail(fmt.Errorf("graph: no condition matched for edges from node %s", node))
		return
	}

	// Register skips (this will trigger skip propagation)
	for _, edge := range skipped {
		t.registerSkip(ctx, node, edge.to)
	}

	// Propagate state along matched edges
	for _, edge := range matched {
		t.propagate(ctx, node, edge.to, state.Clone())
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

// propagate sends state contribution along an edge and schedules target if ready.
func (t *Task) propagate(ctx context.Context, from, to string, state State) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Add contribution
	t.addContributionLocked(to, from, state)

	// Decrement remaining count
	if t.remaining[to] > 0 {
		t.remaining[to]--
	}

	// Check if node is ready
	if t.remaining[to] == 0 && !t.visited[to] && !t.inFlight[to] {
		// Node is ready - add to queue
		t.ready = append(t.ready, to)
	}
}

// registerSkip marks that a parent skipped a target node and propagates skip if needed.
// This implements automatic skip propagation like the original Blades design.
func (t *Task) registerSkip(ctx context.Context, parent, target string) {
	t.mu.Lock()

	if t.visited[target] {
		t.mu.Unlock()
		return
	}

	preds := t.executor.predecessors[target]
	if len(preds) == 0 {
		// No predecessors, nothing to track
		t.mu.Unlock()
		return
	}

	// Mark skip
	if t.skippedFrom[target] == nil {
		t.skippedFrom[target] = make(map[string]bool)
	}
	if t.skippedFrom[target][parent] {
		// Already marked
		t.mu.Unlock()
		return
	}
	t.skippedFrom[target][parent] = true

	// IMPORTANT: Decrement remaining for this skip
	// This is the key insight - skips also decrease the remaining count
	if t.remaining[target] > 0 {
		t.remaining[target]--
	}

	// Check if node is now ready
	if t.remaining[target] == 0 && !t.visited[target] && !t.inFlight[target] {
		hasContributions := len(t.contributions[target]) > 0
		if !hasContributions {
			// All predecessors skipped - mark as skipped and propagate
			t.visited[target] = true
			edges := cloneEdges(t.executor.graph.edges[target])
			t.mu.Unlock()
			// Propagate skip to all children
			for _, edge := range edges {
				t.registerSkip(ctx, target, edge.to)
			}
			return
		}
		// Has contributions - schedule for execution
		t.ready = append(t.ready, target)
	}
	t.mu.Unlock()
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
		// Merge in predecessor order for determinism
		order := t.executor.predecessors[node]
		for _, parent := range order {
			if contribution, exists := contribs[parent]; exists {
				state = mergeStates(state, contribution)
				delete(contribs, parent)
			}
		}
		// Merge any remaining contributions
		for parent, contribution := range contribs {
			state = mergeStates(state, contribution)
			delete(contribs, parent)
		}
		delete(t.contributions, node)
	}
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
