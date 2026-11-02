package gplugin

import (
	"fmt"
	"sort"
	"sync"
)

// Container carries values between capabilities during Register.
//
// It is the smallest untyped store a capability needs. A framework with its own
// container satisfies it without adapters, and an application without one can use a map.
type Container interface {
	Set(key string, value any)
	Get(key string) (any, bool)
}

// Set is the registry of capabilities available to this binary.
//
// It is explicit on purpose: no package-level registry, no panic on duplicates. Wiring
// must be testable in isolation, so a duplicate id is an error the caller sees.
type Set[C any] struct {
	mu      sync.RWMutex
	modules map[string]Module[C]
}

// NewSet returns an empty set.
func NewSet[C any]() *Set[C] {
	return &Set[C]{modules: map[string]Module[C]{}}
}

// Add registers a capability.
//
// A duplicate or empty id is a wiring error: silently replacing a capability would make
// the running binary differ from its manifest.
func (s *Set[C]) Add(module Module[C]) error {
	if module == nil {
		return fmt.Errorf("%w: nil module", ErrPermanent)
	}
	id := module.ID()
	if id == "" {
		return fmt.Errorf("%w: module id must not be empty", ErrPermanent)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modules == nil {
		s.modules = map[string]Module[C]{}
	}
	if _, exists := s.modules[id]; exists {
		return fmt.Errorf("%w: %q", ErrAlreadyRegistered, id)
	}
	s.modules[id] = module
	return nil
}

// IDs lists the available capability ids, sorted.
func (s *Set[C]) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedIDs(s.modules)
}

// Select resolves the requested ids, pulls in their transitive dependencies and orders
// the result topologically.
//
// Every failure is a startup error rather than a silently smaller assembly: an unknown
// capability, a dependency that was not selected, or a cycle all mean the binary does not
// match the declaration.
func (s *Set[C]) Select(ids ...string) (*Assembly[C], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: no capability selected", ErrPermanent)
	}

	selected := map[string]bool{}
	queue := append([]string(nil), ids...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if selected[id] {
			continue
		}
		module, ok := s.modules[id]
		if !ok {
			return nil, fmt.Errorf("%w: capability %q is not in this binary", ErrPermanent, id)
		}
		selected[id] = true
		queue = append(queue, dependsOf(module)...)
	}

	names := make([]string, 0, len(selected))
	for id := range selected {
		names = append(names, id)
	}
	sort.Strings(names)

	deps := make(map[string][]string, len(names))
	for _, id := range names {
		list, missing := cleanDeps(id, dependsOf(s.modules[id]), func(dep string) bool { return selected[dep] })
		if missing != "" {
			return nil, fmt.Errorf("%w: capability %q needs %q, which is not in this binary", ErrPermanent, id, missing)
		}
		deps[id] = list
	}

	ordered, err := order(names, deps)
	if err != nil {
		return nil, err
	}

	modules := make([]Module[C], 0, len(ordered))
	for _, id := range ordered {
		modules = append(modules, s.modules[id])
	}
	return &Assembly[C]{order: ordered, modules: modules}, nil
}
