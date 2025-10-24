package flow

import (
	"context"
	"fmt"
)

// GraphHandler is a function that processes the graph state.
// Handlers must not mutate the incoming state; instead, they should return a new state instance.
// This is especially important for reference types (e.g., pointers, slices, maps) to avoid unintended side effects.
type GraphHandler[S any] func(ctx context.Context, state S) (S, error)

// EdgeCondition is a function that determines if an edge should be followed based on the current state.
type EdgeCondition[S any] func(ctx context.Context, state S) bool

// ActivationCondition declares how incoming edges grouped together trigger a node.
type ActivationCondition string

const (
	ActivationAll ActivationCondition = "all"
	ActivationAny ActivationCondition = "any"

	defaultActivationGroup = ""
)

func normalizeActivationCondition(cond ActivationCondition) ActivationCondition {
	if cond == "" {
		return ActivationAll
	}
	return cond
}

// EdgeOption configures an edge before it is added to the graph.
type EdgeOption[S any] func(*conditionalEdge[S])

// WithEdgeCondition sets a condition that must return true for the edge to be taken.
func WithEdgeCondition[S any](condition EdgeCondition[S]) EdgeOption[S] {
	return func(edge *conditionalEdge[S]) {
		edge.condition = condition
	}
}

// WithActivationGroup assigns the edge to an activation group and condition.
// The group name allows multiple incoming edges to the same target to be evaluated
// together. The activation condition determines whether all or any edges in the
// group must fire before the target node executes.
func WithActivationGroup[S any](group string, condition ActivationCondition) EdgeOption[S] {
	return func(edge *conditionalEdge[S]) {
		edge.activationGroup = group
		edge.activationCondition = condition
	}
}

// conditionalEdge represents an edge with an optional condition.
type conditionalEdge[S any] struct {
	to                  string
	condition           EdgeCondition[S] // nil means always follow this edge
	activationGroup     string
	activationCondition ActivationCondition
}

// Graph represents a directed graph of processing nodes. Cycles are allowed.
type Graph[S any] struct {
	nodes       map[string]GraphHandler[S]
	edges       map[string][]conditionalEdge[S]
	entryPoint  string
	finishPoint string
	incoming    map[string]map[string]*activationGroupMeta
}

type activationGroupMeta struct {
	condition ActivationCondition
	sources   map[string]struct{}
}

// NewGraph creates a new empty Graph.
func NewGraph[S any]() *Graph[S] {
	return &Graph[S]{
		nodes:    make(map[string]GraphHandler[S]),
		edges:    make(map[string][]conditionalEdge[S]),
		incoming: make(map[string]map[string]*activationGroupMeta),
	}
}

// AddNode adds a named node with its handler to the graph.
func (g *Graph[S]) AddNode(name string, handler GraphHandler[S]) error {
	if _, ok := g.nodes[name]; ok {
		return fmt.Errorf("graph: node %s already exists", name)
	}
	g.nodes[name] = handler
	return nil
}

// AddEdge adds a directed edge from one node to another. Options can configure the edge.
func (g *Graph[S]) AddEdge(from, to string, opts ...EdgeOption[S]) error {
	for _, edge := range g.edges[from] {
		if edge.to == to {
			return fmt.Errorf("graph: edge from %s to %s already exists", from, to)
		}
	}
	newEdge := conditionalEdge[S]{to: to}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&newEdge)
	}
	groupName := newEdge.activationGroup
	if groupName == "" {
		groupName = defaultActivationGroup
	}
	cond := normalizeActivationCondition(newEdge.activationCondition)
	metaGroups, ok := g.incoming[to]
	if !ok {
		metaGroups = make(map[string]*activationGroupMeta)
		g.incoming[to] = metaGroups
	}
	meta, exists := metaGroups[groupName]
	if !exists {
		meta = &activationGroupMeta{
			condition: cond,
			sources:   make(map[string]struct{}),
		}
		metaGroups[groupName] = meta
	} else {
		if normalizeActivationCondition(meta.condition) != cond {
			return fmt.Errorf("graph: activation condition conflict for node %s group %q", to, groupName)
		}
	}
	meta.condition = normalizeActivationCondition(meta.condition)
	meta.sources[from] = struct{}{}
	newEdge.activationGroup = groupName
	newEdge.activationCondition = cond
	g.edges[from] = append(g.edges[from], newEdge)
	return nil
}

// SetEntryPoint marks a node as the entry point.
func (g *Graph[S]) SetEntryPoint(start string) error {
	if g.entryPoint != "" {
		return fmt.Errorf("graph: entry point already set to %s", g.entryPoint)
	}
	g.entryPoint = start
	return nil
}

// SetFinishPoint marks a node as the finish point.
func (g *Graph[S]) SetFinishPoint(end string) error {
	if g.finishPoint != "" {
		return fmt.Errorf("graph: finish point already set to %s", g.finishPoint)
	}
	g.finishPoint = end
	return nil
}

// validate ensures the graph configuration is correct before compiling.
func (g *Graph[S]) validate() error {
	if g.entryPoint == "" {
		return fmt.Errorf("graph: entry point not set")
	}
	if g.finishPoint == "" {
		return fmt.Errorf("graph: finish point not set")
	}
	if _, ok := g.nodes[g.entryPoint]; !ok {
		return fmt.Errorf("graph: start node not found: %s", g.entryPoint)
	}
	if _, ok := g.nodes[g.finishPoint]; !ok {
		return fmt.Errorf("graph: end node not found: %s", g.finishPoint)
	}
	for from, edges := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return fmt.Errorf("graph: edge from unknown node: %s", from)
		}
		for _, edge := range edges {
			if _, ok := g.nodes[edge.to]; !ok {
				return fmt.Errorf("graph: edge to unknown node: %s", edge.to)
			}
		}
	}
	return nil
}

// ensureReachable verifies that the finish node can be reached from the entry node.
func (g *Graph[S]) ensureReachable() error {
	if g.entryPoint == g.finishPoint {
		return nil
	}
	queue := []string{g.entryPoint}
	visited := make(map[string]bool, len(g.nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		if node == g.finishPoint {
			return nil
		}
		for _, edge := range g.edges[node] {
			queue = append(queue, edge.to)
		}
	}
	return fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
}

type groupRuntimeState struct {
	condition ActivationCondition
	sources   []string
	satisfied map[string]bool
	pending   bool
}

func (gs *groupRuntimeState) ensureSource(source string) {
	if gs.satisfied == nil {
		gs.satisfied = make(map[string]bool)
	}
	if _, ok := gs.satisfied[source]; ok {
		return
	}
	gs.satisfied[source] = false
	gs.sources = append(gs.sources, source)
}

func (gs *groupRuntimeState) mark(source string) {
	gs.ensureSource(source)
	gs.satisfied[source] = true
}

func (gs *groupRuntimeState) ready() bool {
	if gs.pending {
		return false
	}
	switch normalizeActivationCondition(gs.condition) {
	case ActivationAny:
		for _, src := range gs.sources {
			if gs.satisfied[src] {
				return true
			}
		}
		return false
	default:
		if len(gs.sources) == 0 {
			return true
		}
		for _, src := range gs.sources {
			if !gs.satisfied[src] {
				return false
			}
		}
		return true
	}
}

func (gs *groupRuntimeState) reset() {
	gs.pending = false
	for src := range gs.satisfied {
		gs.satisfied[src] = false
	}
}

type nodeActivationState struct {
	groups map[string]*groupRuntimeState
}

type activationRuntime struct {
	meta  map[string]map[string]*activationGroupMeta
	nodes map[string]*nodeActivationState
}

type executionFrame struct {
	node         string
	group        string
	allowRevisit bool
}

func newActivationRuntime(meta map[string]map[string]*activationGroupMeta) *activationRuntime {
	return &activationRuntime{
		meta:  meta,
		nodes: make(map[string]*nodeActivationState),
	}
}

func (rt *activationRuntime) ensureTracker(node, group string) *groupRuntimeState {
	ns, ok := rt.nodes[node]
	if !ok {
		ns = &nodeActivationState{groups: make(map[string]*groupRuntimeState)}
		rt.nodes[node] = ns
	}
	if group == "" {
		group = defaultActivationGroup
	}
	tracker, ok := ns.groups[group]
	if ok {
		return tracker
	}
	var meta *activationGroupMeta
	if groups, exists := rt.meta[node]; exists {
		meta = groups[group]
	}
	if meta == nil {
		meta = &activationGroupMeta{
			condition: ActivationAll,
			sources:   make(map[string]struct{}),
		}
		if _, exists := rt.meta[node]; !exists {
			rt.meta[node] = make(map[string]*activationGroupMeta)
		}
		rt.meta[node][group] = meta
	}
	tracker = &groupRuntimeState{
		condition: normalizeActivationCondition(meta.condition),
		satisfied: make(map[string]bool, len(meta.sources)),
	}
	for src := range meta.sources {
		tracker.sources = append(tracker.sources, src)
		tracker.satisfied[src] = false
	}
	ns.groups[group] = tracker
	return tracker
}

func (rt *activationRuntime) mark(node, group, source string) *groupRuntimeState {
	tracker := rt.ensureTracker(node, group)
	tracker.mark(source)
	return tracker
}

func (rt *activationRuntime) reset(node, group string) {
	if group == "" {
		group = defaultActivationGroup
	}
	ns, ok := rt.nodes[node]
	if !ok {
		return
	}
	tracker, ok := ns.groups[group]
	if !ok {
		return
	}
	tracker.reset()
}

// Compile validates and compiles the graph into a GraphHandler.
// Execution processes unconditional edges in breadth-first order while allowing
// conditional edges to drive dynamic control flow, including loops.
func (g *Graph[S]) Compile() (GraphHandler[S], error) {
	if err := g.validate(); err != nil {
		return nil, err
	}
	if err := g.ensureReachable(); err != nil {
		return nil, err
	}

	runtime := newActivationRuntime(g.incoming)

	return func(ctx context.Context, state S) (S, error) {
		queue := []executionFrame{{node: g.entryPoint, group: defaultActivationGroup}}
		visited := make(map[string]bool, len(g.nodes))

		for len(queue) > 0 {
			currentFrame := queue[0]
			queue = queue[1:]
			current := currentFrame.node
			group := currentFrame.group

			if visited[current] && !currentFrame.allowRevisit {
				runtime.reset(current, group)
				continue
			}
			visited[current] = true

			handler := g.nodes[current]
			if handler == nil {
				return state, fmt.Errorf("graph: node %s handler missing", current)
			}
			var err error
			state, err = handler(ctx, state)
			if err != nil {
				return state, fmt.Errorf("graph: node %s: %w", current, err)
			}

			runtime.reset(current, group)

			if current == g.finishPoint {
				return state, nil
			}

			edges := g.edges[current]
			if len(edges) == 0 {
				return state, fmt.Errorf("graph: no outgoing edges from node %s", current)
			}

			hasConditional := false
			for _, edge := range edges {
				if edge.condition != nil {
					hasConditional = true
					break
				}
			}

			if hasConditional {
				matched := false
				for _, edge := range edges {
					if edge.condition == nil || edge.condition(ctx, state) {
						g.enqueueEdge(runtime, current, edge, &queue, true)
						matched = true
						break
					}
				}
				if !matched {
					return state, fmt.Errorf("graph: no condition matched for edges from node %s", current)
				}
				continue
			}

			for _, edge := range edges {
				g.enqueueEdge(runtime, current, edge, &queue, false)
			}
		}

		return state, fmt.Errorf("graph: finish node not reachable: %s", g.finishPoint)
	}, nil
}

func (g *Graph[S]) enqueueEdge(runtime *activationRuntime, from string, edge conditionalEdge[S], queue *[]executionFrame, forceImmediate bool) {
	group := edge.activationGroup
	if group == "" {
		group = defaultActivationGroup
	}
	tracker := runtime.mark(edge.to, group, from)
	if tracker.pending {
		return
	}
	if !tracker.ready() {
		return
	}
	tracker.pending = true

	allowRevisit := forceImmediate
	if !allowRevisit && group != defaultActivationGroup {
		allowRevisit = true
	}

	frame := executionFrame{
		node:         edge.to,
		group:        group,
		allowRevisit: allowRevisit,
	}

	if forceImmediate {
		*queue = append([]executionFrame{frame}, *queue...)
	} else {
		*queue = append(*queue, frame)
	}
}
