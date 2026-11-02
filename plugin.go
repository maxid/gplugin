package gplugin

import (
	"fmt"
	"sync"
)

// Plugin is a named extension of a host object.
//
// H is the host the plugin attaches to — a broker, a server, anything the owning library
// decides. Load receives the live host, so a plugin can register callbacks, start
// goroutines and publish through the host's own API; Unload must undo that.
type Plugin[H any] interface {
	// Name identifies the plugin. It is unique within a Registry and is what manifests
	// and error messages report.
	Name() string
	// Load attaches the plugin to the host. Returning an error makes the whole Load
	// fail; plugins loaded before it are unloaded in reverse order.
	Load(H) error
	// Unload detaches the plugin. It is called in reverse load order and must tolerate
	// being called when Load never succeeded.
	Unload() error
}

// Registry is the ordered set of plugins a library offers.
//
// Order comes from the optional Dependent interface and breaks ties on name, so a
// process start is deterministic. A duplicate or empty name is a wiring error rather
// than a silent replacement.
type Registry[H any] struct {
	mu      sync.RWMutex
	plugins map[string]Plugin[H]
	loaded  []Plugin[H]
}

// NewRegistry returns an empty registry.
func NewRegistry[H any]() *Registry[H] {
	return &Registry[H]{plugins: map[string]Plugin[H]{}}
}

// Add registers a plugin without loading it.
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
	}
	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("%w: %q", ErrAlreadyRegistered, name)
	}
	r.plugins[name] = plugin
	return nil
}

// Names lists the registered plugin names, sorted.
func (r *Registry[H]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return sortedIDs(r.plugins)
}

// Loaded lists the loaded plugin names, in load order.
func (r *Registry[H]) Loaded() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.loaded))
	for _, plugin := range r.loaded {
		names = append(names, plugin.Name())
	}
	return names
}

// Manifest reports the registered plugins and the dependency order they load in. It does
// not load anything, so it is safe to call at startup to prove the declaration.
func (r *Registry[H]) Manifest() ([]Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ordered, err := r.orderLocked()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(ordered))
	for _, name := range ordered {
		entries = append(entries, entryOf(name, r.plugins[name]))
	}
	return entries, nil
}

// Load attaches every registered plugin in dependency order.
//
// A failing plugin unloads the ones already loaded, newest first, so a half-loaded host
// never keeps running. A repeated call is an error rather than a second load: loading
// twice would register hooks twice.
func (r *Registry[H]) Load(host H) error {
	r.mu.Lock()
	ordered, err := r.orderLocked()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if len(r.loaded) > 0 {
		already := make([]string, 0, len(r.loaded))
		for _, plugin := range r.loaded {
			already = append(already, plugin.Name())
		}
		r.mu.Unlock()
		return fmt.Errorf("%w: %d plugins are already loaded (%v)", ErrPermanent, len(already), already)
	}
	plugins := make([]Plugin[H], 0, len(ordered))
	for _, name := range ordered {
		plugins = append(plugins, r.plugins[name])
	}
	r.mu.Unlock()

	loaded := make([]Plugin[H], 0, len(plugins))
	for _, plugin := range plugins {
		if loadErr := plugin.Load(host); loadErr != nil {
			_ = unloadPlugins(loaded)
			return fmt.Errorf("gplugin: load %s: %w", plugin.Name(), loadErr)
		}
		loaded = append(loaded, plugin)
	}

	r.mu.Lock()
	r.loaded = loaded
	r.mu.Unlock()
	return nil
}

// Unload detaches every loaded plugin in reverse load order, joining every failure so one
// broken plugin cannot mask the rest.
func (r *Registry[H]) Unload() error {
	r.mu.Lock()
	loaded := r.loaded
	r.loaded = nil
	r.mu.Unlock()
	return unloadPlugins(loaded)
}

// orderLocked returns the load order; the caller holds at least a read lock.
func (r *Registry[H]) orderLocked() ([]string, error) {
	names := sortedIDs(r.plugins)
	deps := make(map[string][]string, len(names))
	for _, name := range names {
		list, missing := cleanDeps(name, dependsOf(r.plugins[name]), func(dep string) bool {
			_, ok := r.plugins[dep]
			return ok
		})
		if missing != "" {
			return nil, fmt.Errorf("%w: plugin %q needs %q, which is not registered", ErrPermanent, name, missing)
		}
		deps[name] = list
	}
	return order(names, deps)
}

// unloadPlugins detaches plugins newest first.
func unloadPlugins[H any](plugins []Plugin[H]) error {
	var failures []error
	for i := len(plugins) - 1; i >= 0; i-- {
		if err := plugins[i].Unload(); err != nil {
			failures = append(failures, fmt.Errorf("gplugin: unload %s: %w", plugins[i].Name(), err))
		}
	}
	return joinErrors(failures)
}
