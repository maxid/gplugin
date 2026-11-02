package gplugin

import (
	"fmt"
	"sort"
)

// order sorts ids so that every dependency comes before its dependents.
//
// Ties break on id, so the order is identical on every process start: a rolling restart
// must never reorder what is wired. deps must stay inside ids — a dependency outside the
// set is the caller's error to report with its own context.
func order(ids []string, deps map[string][]string) ([]string, error) {
	indegree := make(map[string]int, len(ids))
	ready := make([]string, 0, len(ids))
	for _, id := range ids {
		indegree[id] = len(deps[id])
		if len(deps[id]) == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ordered := make([]string, 0, len(ids))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, id)

		for _, other := range ids {
			for _, dep := range deps[other] {
				if dep != id {
					continue
				}
				indegree[other]--
				if indegree[other] == 0 {
					ready = append(ready, other)
				}
			}
		}
		sort.Strings(ready)
	}

	if len(ordered) != len(ids) {
		return nil, fmt.Errorf("%w: dependency cycle", ErrPermanent)
	}
	return ordered, nil
}

// cleanDeps drops empty, self-referential and duplicate declarations, and names the first
// dependency that is not in the set. The caller decides how to phrase that as an error,
// because "not selected" and "not registered" read differently.
func cleanDeps(id string, declared []string, inSet func(string) bool) (list []string, missing string) {
	seen := make(map[string]bool, len(declared))
	list = make([]string, 0, len(declared))
	for _, dep := range declared {
		if dep == "" || dep == id || seen[dep] {
			continue
		}
		if !inSet(dep) {
			return nil, dep
		}
		seen[dep] = true
		list = append(list, dep)
	}
	return list, ""
}

// dependsOf reads an optional Dependent.
func dependsOf(value any) []string {
	if dependent, ok := value.(Dependent); ok {
		return dependent.DependsOn()
	}
	return nil
}

// sortedIDs returns the keys of a name-keyed map in sorted order, so both layers report
// and order their contents deterministically.
func sortedIDs[V any](entries map[string]V) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
