package gplugin

import (
	"fmt"
	"sort"
	"sync"
)

// Plugin is a named extension of a host object.
//
// H is the host the plugin attaches to — a broker, a server, anything the owning library
// decides. Start receives the live host, so a plugin can register callbacks, start
// goroutines and publish through the host's own API; Stop must undo that.
type Plugin[H any] interface {
	// Name identifies the plugin. It is unique within a Registry and is what status
	// output and error messages report.
	Name() string
	// Start attaches the plugin to the host. Returning an error leaves the plugin
	// stopped, records the reason and rolls back the plugins this call started.
	Start(H) error
	// Stop detaches the plugin. It is called in reverse start order and must tolerate
	// being called when Start never succeeded.
	Stop() error
}

// Registry is the ordered set of plugins a library offers and controls at run time.
//
// Order comes from the optional Dependent interface and breaks ties on name, so a process
// start is deterministic. A duplicate or empty name is a wiring error rather than a silent
// replacement.
type Registry[H any] struct {
	host      H
	observers []Observer

	// opMu serializes Start and Stop so two control calls cannot interleave and leave the
	// dependency graph half-switched.
	opMu sync.Mutex
	mu   sync.RWMutex

	plugins map[string]Plugin[H]
	states  map[string]State
	errs    map[string]error
}

// NewRegistry returns a registry bound to host.
//
// The host is fixed for the registry's lifetime: it is what every plugin's Start receives,
// whether the plugin starts at boot or later from a control call.
func NewRegistry[H any](host H, opts ...Option) *Registry[H] {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return &Registry[H]{
		host:      host,
		observers: o.observers,
		plugins:   map[string]Plugin[H]{},
		states:    map[string]State{},
		errs:      map[string]error{},
	}
}

// Add registers a plugin without starting it. Its state becomes StateRegistered.
func (r *Registry[H]) Add(plugin Plugin[H]) error {
	if plugin == nil {
		return fmt.Errorf("%w: nil plugin", ErrPermanent)
	}
	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("%w: plugin name must not be empty", ErrPermanent)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.plugins == nil {
		r.plugins = map[string]Plugin[H]{}
		r.states = map[string]State{}
		r.errs = map[string]error{}
	}
	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("%w: %q", ErrAlreadyRegistered, name)
	}
	r.plugins[name] = plugin
	r.states[name] = StateRegistered
	return nil
}

// Names lists the registered plugin names, sorted.
func (r *Registry[H]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedIDs(r.plugins)
}

// Enabled lists the running plugin names, in start order.
func (r *Registry[H]) Enabled() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ordered, err := r.orderLocked()
	if err != nil {
		// A broken dependency graph is reported by Snapshot and Start; Enabled still
		// answers honestly, in name order.
		ordered = sortedIDs(r.plugins)
	}
	enabled := make([]string, 0, len(ordered))
	for _, name := range ordered {
		if r.states[name] == StateEnabled {
			enabled = append(enabled, name)
		}
	}
	return enabled
}

// State reports where one plugin stands. The second result is false when the name is not
// registered.
func (r *Registry[H]) State(name string) (State, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.states[name]
	return state, ok
}

// Snapshot reports every registered plugin in start order, with its state and the reason
// for its last failure.
//
// It has no side effects: nothing is started and no event is emitted, so it is safe to
// call at any point — which is what makes "print what the process enabled" a one-liner for
// the application.
func (r *Registry[H]) Snapshot() ([]Status, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ordered, err := r.orderLocked()
	if err != nil {
		return nil, err
	}
	statuses := make([]Status, 0, len(ordered))
	for _, name := range ordered {
		status := entryOf(name, r.plugins[name])
		statuses = append(statuses, Status{
			ID:        status.ID,
			Version:   status.Version,
			DependsOn: status.DependsOn,
			State:     r.states[name],
			Err:       r.errs[name],
		})
	}
	return statuses, nil
}

// Start starts the named plugins and everything they depend on, in dependency order.
//
// An already running plugin is left alone and produces no event: the operation is
// idempotent so a control call can be retried. A dependency that is not registered is an
// error, never a silently smaller start. When a plugin fails to start, the plugins this
// call started are stopped again in reverse order, the failure is recorded and observed,
// and the error names the plugin that failed.
func (r *Registry[H]) Start(names ...string) error {
	if len(names) == 0 {
		return fmt.Errorf("%w: no plugin named", ErrPermanent)
	}
	return r.start(names)
}

// StartAll starts every registered plugin, in dependency order.
func (r *Registry[H]) StartAll() error {
	r.mu.RLock()
	names := sortedIDs(r.plugins)
	r.mu.RUnlock()
	if len(names) == 0 {
		return nil
	}
	return r.start(names)
}

// Stop stops the named plugins and every running plugin that depends on them, newest
// first.
//
// Stopping something that is not running is a no-op. A failure does not abort the
// operation — the remaining plugins are still stopped and the failures are joined —
// because a half-stopped dependency graph is more dangerous than a logged error. A plugin
// whose Stop failed stays enabled with the reason recorded: it did not confirm that it
// stopped, and pretending otherwise would hide a half-detached plugin.
func (r *Registry[H]) Stop(names ...string) error {
	if len(names) == 0 {
		return fmt.Errorf("%w: no plugin named", ErrPermanent)
	}
	return r.stop(names)
}

// StopAll stops every running plugin, newest first.
func (r *Registry[H]) StopAll() error {
	r.mu.RLock()
	names := sortedIDs(r.plugins)
	r.mu.RUnlock()
	return r.stop(names)
}

// start runs the closure of names in dependency order.
func (r *Registry[H]) start(names []string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	ordered, err := r.startPlan(names)
	if err != nil {
		return err
	}

	var events []Event
	started := make([]string, 0, len(ordered))
	for _, name := range ordered {
		from, plugin := r.current(name)
		if from == StateEnabled {
			continue // already running
		}
		if startErr := plugin.Start(r.host); startErr != nil {
			r.record(name, startErr)
			events = append(events, Event{ID: name, From: from, To: from, Err: startErr})
			events = append(events, r.stopStarted(started)...)
			r.emit(events)
			return fmt.Errorf("gplugin: start %s: %w", name, startErr)
		}
		r.setState(name, StateEnabled)
		events = append(events, Event{ID: name, From: from, To: StateEnabled})
		started = append(started, name)
	}
	r.emit(events)
	return nil
}

// stopStarted rolls back the plugins this call started, newest first.
func (r *Registry[H]) stopStarted(started []string) []Event {
	events := make([]Event, 0, len(started))
	for i := len(started) - 1; i >= 0; i-- {
		name := started[i]
		from, plugin := r.current(name)
		if from != StateEnabled {
			continue
		}
		stopErr := plugin.Stop()
		if stopErr != nil {
			r.record(name, stopErr)
			events = append(events, Event{ID: name, From: from, To: from, Err: stopErr})
			continue
		}
		r.setState(name, StateStopped)
		events = append(events, Event{ID: name, From: from, To: StateStopped})
	}
	return events
}

// stop runs the dependency closure of names in reverse dependency order.
func (r *Registry[H]) stop(names []string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	ordered, err := r.stopPlan(names)
	if err != nil {
		return err
	}

	var events []Event
	var failures []error
	for i := len(ordered) - 1; i >= 0; i-- {
		name := ordered[i]
		from, plugin := r.current(name)
		if from != StateEnabled {
			continue // not running
		}
		stopErr := plugin.Stop()
		if stopErr != nil {
			r.record(name, stopErr)
			events = append(events, Event{ID: name, From: from, To: from, Err: stopErr})
			failures = append(failures, fmt.Errorf("gplugin: stop %s: %w", name, stopErr))
			continue
		}
		r.setState(name, StateStopped)
		events = append(events, Event{ID: name, From: from, To: StateStopped})
	}
	r.emit(events)
	return joinErrors(failures)
}

// current returns a plugin and its state, so the caller can decide whether the action is a
// no-op (already running for start, not running for stop).
func (r *Registry[H]) current(name string) (State, Plugin[H]) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.states[name], r.plugins[name]
}

// setState records a successful transition and clears the previous failure.
func (r *Registry[H]) setState(name string, state State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[name] = state
	delete(r.errs, name)
}

// record keeps the reason a transition failed, without changing the state.
func (r *Registry[H]) record(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs[name] = err
}

// emit delivers the transitions to every observer, in order, with no lock held.
func (r *Registry[H]) emit(events []Event) {
	if len(events) == 0 || len(r.observers) == 0 {
		return
	}
	for _, event := range events {
		for _, observer := range r.observers {
			if observer != nil {
				observer.StateChanged(event)
			}
		}
	}
}

// startPlan resolves names plus their dependencies and orders them to start.
func (r *Registry[H]) startPlan(names []string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	closure, err := r.closureLocked(names)
	if err != nil {
		return nil, err
	}
	return r.orderForLocked(closure)
}

// stopPlan resolves names plus every plugin that depends on them and orders them to stop.
func (r *Registry[H]) stopPlan(names []string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range names {
		if _, ok := r.plugins[name]; !ok {
			return nil, fmt.Errorf("%w: plugin %q is not registered", ErrPermanent, name)
		}
	}
	// Reverse closure: everything that reaches one of names through DependsOn.
	closure := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for name := range r.plugins {
			if closure[name] {
				continue
			}
			if containsName(names, name) {
				closure[name] = true
				changed = true
				continue
			}
			for _, dep := range dependsOf(r.plugins[name]) {
				if closure[dep] {
					closure[name] = true
					changed = true
					break
				}
			}
		}
	}
	return r.orderSubsetLocked(closure)
}

// closureLocked adds every transitive dependency of names.
func (r *Registry[H]) closureLocked(names []string) (map[string]bool, error) {
	closure := map[string]bool{}
	queue := append([]string(nil), names...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if closure[name] {
			continue
		}
		plugin, ok := r.plugins[name]
		if !ok {
			return nil, fmt.Errorf("%w: plugin %q is not registered", ErrPermanent, name)
		}
		closure[name] = true
		queue = append(queue, dependsOf(plugin)...)
	}
	return closure, nil
}

// orderForLocked orders a subset of the registered plugins; the caller holds a lock.
func (r *Registry[H]) orderForLocked(closure map[string]bool) ([]string, error) {
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	deps := make(map[string][]string, len(names))
	for _, name := range names {
		list, missing := cleanDeps(name, dependsOf(r.plugins[name]), func(dep string) bool {
			return closure[dep]
		})
		if missing != "" {
			return nil, fmt.Errorf("%w: plugin %q needs %q, which is not registered", ErrPermanent, name, missing)
		}
		deps[name] = list
	}
	return order(names, deps)
}

// orderSubsetLocked orders a subset whose dependencies may live outside it — the stop
// path, where a dependency that keeps running is exactly the point. Dependencies outside
// the subset are ignored rather than treated as missing.
func (r *Registry[H]) orderSubsetLocked(closure map[string]bool) ([]string, error) {
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)
	deps := make(map[string][]string, len(names))
	for _, name := range names {
		kept := make([]string, 0, 2)
		for _, dep := range dependsOf(r.plugins[name]) {
			if dep != name && closure[dep] && !containsName(kept, dep) {
				kept = append(kept, dep)
			}
		}
		deps[name] = kept
	}
	return order(names, deps)
}

// orderLocked returns the start order of every registered plugin.
func (r *Registry[H]) orderLocked() ([]string, error) {
	closure := make(map[string]bool, len(r.plugins))
	for name := range r.plugins {
		closure[name] = true
	}
	return r.orderForLocked(closure)
}

// containsName reports whether list holds name.
func containsName(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}
