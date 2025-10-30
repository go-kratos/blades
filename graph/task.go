package graph

import (
	"context"
	"fmt"
	"sync"
)

// Task coordinates a single execution of the graph.
type Task struct {
	executor *Executor

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup

	mu            sync.Mutex
	aggregates    map[string]State
	contributions map[string]map[string]State
	remainingDeps map[string]map[string]int
	inFlight      map[string]bool
	visited       map[string]bool
	skippedCnt    map[string]int
	skippedFrom   map[string]map[string]bool

	finished    bool
	finishState State
	err         error
	errOnce     sync.Once
}

func newTask(e *Executor) *Task {
	return &Task{
		executor:      e,
		aggregates:    make(map[string]State),
		contributions: make(map[string]map[string]State),
		remainingDeps: cloneDependencies(e.dependencies),
		inFlight:      make(map[string]bool, len(e.graph.nodes)),
		visited:       make(map[string]bool, len(e.graph.nodes)),
		skippedCnt:    make(map[string]int, len(e.graph.nodes)),
		skippedFrom:   make(map[string]map[string]bool, len(e.graph.nodes)),
	}
}

func (t *Task) run(ctx context.Context, initial State) (State, error) {
	taskCtx, cancel := context.WithCancel(ctx)
	t.ctx = taskCtx
	t.cancel = cancel
	defer cancel()

	t.storeAggregate(t.executor.graph.entryPoint, initial)
	t.trySchedule(t.executor.graph.entryPoint)

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

func (t *Task) storeAggregate(node string, state State) {
	if state == nil {
		state = State{}
	}
	t.mu.Lock()
	t.aggregates[node] = mergeStates(t.aggregates[node], state)
	t.mu.Unlock()
}

func (t *Task) trySchedule(node string) {
	t.mu.Lock()
	if t.ctx.Err() != nil || t.err != nil || t.finished {
		t.mu.Unlock()
		return
	}
	if t.visited[node] || t.inFlight[node] {
		t.mu.Unlock()
		return
	}
	if !t.dependenciesSatisfiedLocked(node) {
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
		t.executeNode(node, state)
	}

	if parallel {
		go run()
	} else {
		run()
	}
}

func (t *Task) executeNode(node string, state State) {
	if state == nil {
		state = State{}
	}

	if err := t.ctx.Err(); err != nil {
		return
	}

	handler := t.executor.graph.nodes[node]
	if handler == nil {
		t.fail(fmt.Errorf("graph: node %s handler missing", node))
		return
	}

	if len(t.executor.graph.middlewares) > 0 {
		handler = ChainMiddlewares(t.executor.graph.middlewares...)(handler)
	}

	nextState, err := handler(t.ctx, state)
	if err != nil {
		t.fail(fmt.Errorf("graph: node %s: %w", node, err))
		return
	}

	nextState = nextState.Clone()

	if t.markVisited(node, nextState) {
		return
	}

	t.processOutgoing(node, nextState)
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

	if isFinish {
		t.cancel()
	}

	return finished && isFinish
}

func (t *Task) processOutgoing(node string, state State) {
	edges := t.executor.graph.edges[node]
	if len(edges) == 0 {
		t.fail(fmt.Errorf("graph: no outgoing edges from node %s", node))
		return
	}

	matched, skipped, err := resolveEdgeSelection(t.ctx, node, edges, state)
	if err != nil {
		t.fail(err)
		return
	}

	for _, edge := range skipped {
		t.registerSkip(node, edge)
	}

	for _, edge := range matched {
		ready := t.consumeAndAggregate(node, edge.to, edge.group, state.Clone())
		if ready {
			t.trySchedule(edge.to)
		}
	}
}

func (t *Task) consumeAndAggregate(parent, target, group string, contribution State) bool {
	t.mu.Lock()
	if contribution != nil {
		if t.contributions[target] == nil {
			t.contributions[target] = make(map[string]State)
		}
		t.contributions[target][parent] = contribution.Clone()
	}
	ready := t.consumeDependencyLocked(target, group) && !t.visited[target]
	t.mu.Unlock()
	return ready
}

func (t *Task) registerSkip(parent string, edge conditionalEdge) {
	target := edge.to

	t.mu.Lock()
	if t.visited[target] {
		t.mu.Unlock()
		return
	}

	t.consumeDependencyLocked(target, edge.group)

	preds := t.executor.predecessors[target]
	if len(preds) == 0 {
		t.mu.Unlock()
		return
	}
	if t.skippedFrom[target] == nil {
		t.skippedFrom[target] = make(map[string]bool)
	}
	if t.skippedFrom[target][parent] {
		t.mu.Unlock()
		return
	}
	t.skippedFrom[target][parent] = true
	t.skippedCnt[target]++

	allSkipped := t.skippedCnt[target] >= len(preds)
	hasState := t.hasStateLocked(target)
	ready := t.dependenciesSatisfiedLocked(target)
	t.mu.Unlock()

	if allSkipped {
		t.markNodeSkipped(target)
		return
	}
	if ready && hasState {
		t.trySchedule(target)
	}
}

func (t *Task) markNodeSkipped(node string) {
	t.mu.Lock()
	if t.visited[node] {
		t.mu.Unlock()
		return
	}
	t.visited[node] = true
	edges := cloneEdges(t.executor.graph.edges[node])
	t.mu.Unlock()

	for _, edge := range edges {
		t.registerSkip(node, edge)
	}
}

func (t *Task) nodeDone(node string) {
	t.mu.Lock()
	delete(t.inFlight, node)
	t.mu.Unlock()
	t.wg.Done()
}

func (t *Task) fail(err error) {
	if err == nil {
		return
	}
	t.errOnce.Do(func() {
		t.mu.Lock()
		t.err = err
		t.mu.Unlock()
		t.cancel()
	})
}

func (t *Task) buildAggregateLocked(node string) State {
	state := t.aggregates[node]

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

	delete(t.aggregates, node)
	if state == nil {
		return State{}
	}
	return state.Clone()
}

func (t *Task) dependenciesSatisfiedLocked(node string) bool {
	groups := t.remainingDeps[node]
	if len(groups) == 0 {
		return true
	}
	for _, count := range groups {
		if count > 0 {
			return false
		}
	}
	return true
}

func (t *Task) consumeDependencyLocked(node, group string) bool {
	if group == "" {
		group = node
	}

	groups := t.remainingDeps[node]
	if len(groups) == 0 {
		return true
	}
	if count, ok := groups[group]; ok {
		if count > 0 {
			groups[group] = count - 1
		}
	}
	return t.dependenciesSatisfiedLocked(node)
}

func (t *Task) hasStateLocked(node string) bool {
	if t.aggregates[node] != nil {
		return true
	}
	if contribs, ok := t.contributions[node]; ok {
		return len(contribs) > 0
	}
	return false
}

func resolveEdgeSelection(ctx context.Context, node string, edges []conditionalEdge, state State) ([]conditionalEdge, []conditionalEdge, error) {
	if len(edges) == 0 {
		return nil, nil, fmt.Errorf("graph: no outgoing edges from node %s", node)
	}

	allUnconditional := true
	for _, edge := range edges {
		if edge.condition != nil {
			allUnconditional = false
			break
		}
	}
	if allUnconditional {
		return cloneEdges(edges), nil, nil
	}

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
				hasTrailing := false
				for _, trailing := range edges[i+1:] {
					if trailing.condition == nil {
						hasTrailing = true
						break
					}
				}
				if hasTrailing {
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
		return nil, nil, fmt.Errorf("graph: no condition matched for edges from node %s", node)
	}

	if hasUnconditional && len(matched) > 1 {
		// When an unconditional edge follows conditionals, only the first match is used.
		matched = matched[:1]
	}

	return cloneEdges(matched), skipped, nil
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
