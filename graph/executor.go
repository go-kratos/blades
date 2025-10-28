package graph

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Executor represents a compiled graph ready for execution.
type Executor struct {
	graph       *Graph
	queue       []graphFrame
	waiting     map[string]int
	visited     map[string]bool
	finished    bool
	finishState State
	// latestState tracks the most recent execution state, used only in serial mode
	// for state accumulation across sequentially executed branches
	latestState State
}

type graphFrame struct {
	node         string
	state        State
	allowRevisit bool
}

type edgeResolution struct {
	immediate []graphFrame
	fanOut    []conditionalEdge
	prepend   bool
}

type branchResult struct {
	idx   int
	state State
}

func newExecutor(g *Graph, state State) *Executor {
	normalized := state.Clone()
	return &Executor{
		graph:       g,
		queue:       []graphFrame{{node: g.entryPoint, state: normalized}},
		waiting:     make(map[string]int),
		visited:     make(map[string]bool, len(g.nodes)),
		latestState: normalized,
	}
}

// Execute runs the graph execution starting from the given state.
func (e *Executor) Execute(ctx context.Context, state State) (State, error) {
	e.reset(state)
	for len(e.queue) > 0 {
		frame := e.dequeue()

		if e.shouldSkip(frame) {
			continue
		}

		nextState, err := e.executeNode(ctx, frame)
		if err != nil {
			return frame.state, err
		}

		if e.handleFinish(frame.node, nextState) {
			continue
		}

		if err := e.processOutgoingEdges(ctx, frame, nextState); err != nil {
			return nextState, err
		}
	}

	if e.finished {
		return e.finishState, nil
	}
	return nil, fmt.Errorf("graph: finish node not reachable: %s", e.graph.finishPoint)
}

func (e *Executor) reset(state State) {
	normalized := state.Clone()
	e.queue = []graphFrame{{node: e.graph.entryPoint, state: normalized}}
	e.waiting = make(map[string]int)
	e.visited = make(map[string]bool, len(e.graph.nodes))
	e.finished = false
	e.finishState = nil
	e.latestState = normalized
}

func (e *Executor) dequeue() graphFrame {
	frame := e.queue[0]
	e.queue = e.queue[1:]
	return frame
}

func (e *Executor) shouldSkip(frame graphFrame) bool {
	// Defer if waiting for other edges
	if e.waiting[frame.node] > 0 && !frame.allowRevisit {
		e.queue = append(e.queue, frame)
		return true
	}
	// Skip if already visited
	if e.visited[frame.node] && !frame.allowRevisit {
		return true
	}
	return false
}

func (e *Executor) executeNode(ctx context.Context, frame graphFrame) (State, error) {
	localState := e.stateFor(frame).Clone()
	handler := e.graph.nodes[frame.node]
	if handler == nil {
		return nil, fmt.Errorf("graph: node %s handler missing", frame.node)
	}

	nextState, err := handler(ctx, localState)
	if err != nil {
		return nil, fmt.Errorf("graph: node %s: %w", frame.node, err)
	}

	e.visited[frame.node] = true
	// Update latestState for serial mode state accumulation
	e.latestState = nextState.Clone()
	return nextState.Clone(), nil
}

func (e *Executor) handleFinish(node string, state State) bool {
	if node == e.graph.finishPoint {
		e.finished = true
		e.finishState = state.Clone()
		return true
	}
	return false
}

func (e *Executor) processOutgoingEdges(ctx context.Context, frame graphFrame, state State) error {
	resolution, err := e.resolveEdges(ctx, frame, state)
	if err != nil {
		return err
	}

	// Handle immediate transitions (single matched conditional edge)
	if len(resolution.immediate) > 0 {
		e.enqueueFrames(resolution.immediate, resolution.prepend)
		return nil
	}

	edges := resolution.fanOut

	// Serial mode: enqueue edges sequentially
	if !e.graph.parallel {
		e.fanOutSerial(frame, edges)
		return nil
	}

	// Single edge: no need for parallel execution
	if len(edges) == 1 {
		e.enqueue(graphFrame{
			node:         edges[0].to,
			state:        state.Clone(),
			allowRevisit: frame.allowRevisit,
		})
		return nil
	}

	// Multiple edges: execute in parallel
	_, err = e.fanOutParallel(ctx, frame, state, edges)
	if err != nil {
		return err
	}
	return nil
}

func (e *Executor) stateFor(frame graphFrame) State {
	if frame.state != nil {
		return frame.state
	}
	return e.latestState
}

func (e *Executor) enqueue(frame graphFrame) {
	e.queue = append(e.queue, frame)
}

func (e *Executor) enqueueFrames(frames []graphFrame, prepend bool) {
	if len(frames) == 0 {
		return
	}
	if prepend {
		e.queue = append(frames, e.queue...)
		return
	}
	e.queue = append(e.queue, frames...)
}

func (e *Executor) resolveEdges(ctx context.Context, frame graphFrame, state State) (edgeResolution, error) {
	edges := e.graph.edges[frame.node]
	if len(edges) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no outgoing edges from node %s", frame.node)
	}

	// Classify edges: all conditional, all unconditional, or mixed
	conditionalEdges, unconditionalEdges := e.classifyEdges(edges)

	// Case 1: All edges are unconditional - fan out to all
	if len(conditionalEdges) == 0 {
		return edgeResolution{fanOut: edges}, nil
	}

	// Case 2: All edges are conditional - evaluate and fan out to matches
	if len(unconditionalEdges) == 0 {
		return e.resolveAllConditional(ctx, state, conditionalEdges, frame.node)
	}

	// Case 3: Mixed edges - evaluate in order, first match wins (conditional or unconditional)
	return e.resolveMixed(ctx, state, edges)
}

// classifyEdges separates edges into conditional and unconditional
func (e *Executor) classifyEdges(edges []conditionalEdge) (conditional, unconditional []conditionalEdge) {
	for _, edge := range edges {
		if edge.condition != nil {
			conditional = append(conditional, edge)
		} else {
			unconditional = append(unconditional, edge)
		}
	}
	return
}

// resolveAllConditional handles the case where all edges are conditional
func (e *Executor) resolveAllConditional(ctx context.Context, state State, edges []conditionalEdge, nodeName string) (edgeResolution, error) {
	matched := make([]conditionalEdge, 0, len(edges))
	for _, edge := range edges {
		if edge.condition(ctx, state) {
			matched = append(matched, edge)
		}
	}
	if len(matched) == 0 {
		return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", nodeName)
	}
	// Single match - take it immediately
	if len(matched) == 1 {
		return edgeResolution{
			immediate: []graphFrame{{
				node:         matched[0].to,
				state:        state.Clone(),
				allowRevisit: true,
			}},
		}, nil
	}
	// Multiple matches - fan out
	return edgeResolution{fanOut: matched}, nil
}

// resolveMixed handles the case where edges are a mix of conditional and unconditional
// First match wins (conditional edges are checked first, then unconditional)
func (e *Executor) resolveMixed(ctx context.Context, state State, edges []conditionalEdge) (edgeResolution, error) {
	for _, edge := range edges {
		if edge.condition == nil || edge.condition(ctx, state) {
			return edgeResolution{
				immediate: []graphFrame{{
					node:         edge.to,
					state:        state.Clone(),
					allowRevisit: true,
				}},
				prepend: true,
			}, nil
		}
	}
	return edgeResolution{}, fmt.Errorf("graph: no condition matched for edges from node %s", edges[0].to)
}

func (e *Executor) fanOutSerial(frame graphFrame, edges []conditionalEdge) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}
	for _, edge := range edges {
		e.waiting[edge.to]--
		if e.waiting[edge.to] == 0 {
			e.enqueue(graphFrame{
				node:         edge.to,
				state:        nil, // Use latestState for serial execution
				allowRevisit: frame.allowRevisit,
			})
		}
	}
}

func (e *Executor) fanOutParallel(ctx context.Context, frame graphFrame, state State, edges []conditionalEdge) (State, error) {
	for _, edge := range edges {
		e.waiting[edge.to]++
	}

	for _, edge := range edges {
		for _, nextEdge := range e.graph.edges[edge.to] {
			e.waiting[nextEdge.to]++
		}
	}

	results := make([]branchResult, len(edges))
	eg, egCtx := errgroup.WithContext(ctx)
	for i, edge := range edges {
		i := i
		edge := edge
		eg.Go(func() error {
			handler := e.graph.nodes[edge.to]
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

	successorStates := make(map[string]State)
	pending := make(map[string]State)
	mergedBranches := state.Clone()
	for _, result := range results {
		edge := edges[result.idx]
		e.waiting[edge.to]--

		branchEdges := e.graph.edges[edge.to]
		for _, nextEdge := range branchEdges {
			e.waiting[nextEdge.to]--
			pending[nextEdge.to] = mergeStates(pending[nextEdge.to], result.state)
			if e.waiting[nextEdge.to] == 0 {
				successorStates[nextEdge.to] = pending[nextEdge.to].Clone()
				delete(pending, nextEdge.to)
			}
		}

		mergedBranches = mergeStates(mergedBranches, result.state)
		e.visited[edge.to] = true
	}

	for successor, successorState := range successorStates {
		e.enqueue(graphFrame{
			node:         successor,
			state:        successorState.Clone(),
			allowRevisit: frame.allowRevisit,
		})
	}

	return mergedBranches, nil
}

// mergeStates merges states at the first level only.
// Each handler manages state at the key level, so we only merge top-level keys.
// Later updates override earlier values for the same key.
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
