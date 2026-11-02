package gplugin

import (
	"context"
	"fmt"
)

// Assembly is a selected, ordered set of capabilities.
type Assembly[C any] struct {
	order   []string
	modules []Module[C]
}

// Manifest reports what the assembly contains, in start order.
func (a *Assembly[C]) Manifest() []Entry {
	entries := make([]Entry, 0, len(a.modules))
	for _, module := range a.modules {
		entries = append(entries, entryOf(module.ID(), module))
	}
	return entries
}

// IDs returns the capability ids in start order.
func (a *Assembly[C]) IDs() []string { return append([]string(nil), a.order...) }

// Register runs every capability's registration in dependency order.
func (a *Assembly[C]) Register(container C) error {
	for _, module := range a.modules {
		if err := module.Register(container); err != nil {
			return fmt.Errorf("gplugin: register %s: %w", module.ID(), err)
		}
	}
	return nil
}

// Start registers and then starts the capabilities in order.
//
// A failure stops the ones already started, in reverse order, so a half-started
// application never keeps serving.
func (a *Assembly[C]) Start(ctx context.Context, container C) error {
	if err := a.Register(container); err != nil {
		return err
	}

	started := make([]Module[C], 0, len(a.modules))
	for _, module := range a.modules {
		if err := ctx.Err(); err != nil {
			_ = stopModules(ctx, started)
			return err
		}
		starter, ok := module.(Starter)
		if !ok {
			continue
		}
		if err := starter.Start(ctx); err != nil {
			_ = stopModules(ctx, started)
			return fmt.Errorf("gplugin: start %s: %w", module.ID(), err)
		}
		started = append(started, module)
	}
	return nil
}

// Stop shuts the capabilities down in reverse order, joining every failure so one broken
// capability cannot mask the rest.
func (a *Assembly[C]) Stop(ctx context.Context) error {
	return stopModules(ctx, a.modules)
}

// stopModules stops whatever was started, newest first.
func stopModules[C any](ctx context.Context, modules []Module[C]) error {
	var failures []error
	for i := len(modules) - 1; i >= 0; i-- {
		stopper, ok := modules[i].(Stopper)
		if !ok {
			continue
		}
		if err := stopper.Stop(ctx); err != nil {
			failures = append(failures, fmt.Errorf("gplugin: stop %s: %w", modules[i].ID(), err))
		}
	}
	return joinErrors(failures)
}
