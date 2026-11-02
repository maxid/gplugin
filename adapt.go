package gplugin

import (
	"context"
	"fmt"
)

// Static wraps an already-built capability under an explicit id and version.
//
// It exists so an application can adopt selection and manifests without rewriting its
// modules: the wrapped module keeps its own Register and lifecycle.
func Static[C any](id, version string, module Module[C]) Module[C] {
	return staticModule[C]{id: id, version: version, inner: module}
}

type staticModule[C any] struct {
	id      string
	version string
	inner   Module[C]
}

func (m staticModule[C]) ID() string      { return m.id }
func (m staticModule[C]) Version() string { return m.version }

func (m staticModule[C]) Register(container C) error {
	if m.inner == nil {
		return nil
	}
	return m.inner.Register(container)
}

// Start forwards to the wrapped module when it has a lifecycle.
func (m staticModule[C]) Start(ctx context.Context) error {
	if starter, ok := m.inner.(Starter); ok {
		return starter.Start(ctx)
	}
	return nil
}

// Stop forwards to the wrapped module when it has a lifecycle.
func (m staticModule[C]) Stop(ctx context.Context) error {
	if stopper, ok := m.inner.(Stopper); ok {
		return stopper.Stop(ctx)
	}
	return nil
}

// DependsOn forwards the wrapped module's dependencies.
func (m staticModule[C]) DependsOn() []string { return dependsOf(m.inner) }

// Spec describes a capability by closures instead of a type.
//
// It exists for modules that already have their own shape — a module whose Register takes
// a concrete container type, or that has its own lifecycle interface, cannot satisfy this
// package's interfaces by signature. Describing such a module with closures lets it join
// selection and manifests without renaming anything in its own repository.
type Spec[C any] struct {
	// ID is the capability name used for selection.
	ID string
	// Version is reported in the manifest; empty means the module does not declare one.
	Version string
	// DependsOn lists the capabilities that must start first.
	DependsOn []string
	// Register wires the capability. The container is this package's interface; a module
	// whose Register takes its own concrete type adapts it inside this closure.
	Register func(C) error
	// Start and Stop are optional; nil means the capability has no lifecycle.
	Start func(context.Context) error
	Stop  func(context.Context) error
}

// From turns a Spec into a Module.
//
// No description is inferred: everything the manifest shows comes from the Spec, so an
// adapted module's version and dependencies must be stated rather than guessed. A spec
// without hooks becomes a wiring-only module — it does not satisfy Starter, so selection
// never lists it as started and a rollback has nothing to stop for it.
func From[C any](spec Spec[C]) (Module[C], error) {
	if spec.ID == "" {
		return nil, fmt.Errorf("%w: capability id is required", ErrPermanent)
	}
	if spec.Register == nil {
		return nil, fmt.Errorf("%w: capability %q has no register function", ErrPermanent, spec.ID)
	}
	base := specModule[C]{spec: spec}
	if spec.Start == nil && spec.Stop == nil {
		return base, nil
	}
	return specLifecycle[C]{base}, nil
}

// specModule is the wiring half of an adapted spec: identity, version and dependencies.
type specModule[C any] struct {
	spec Spec[C]
}

func (m specModule[C]) ID() string          { return m.spec.ID }
func (m specModule[C]) Version() string     { return m.spec.Version }
func (m specModule[C]) DependsOn() []string { return append([]string(nil), m.spec.DependsOn...) }
func (m specModule[C]) Register(c C) error  { return m.spec.Register(c) }

// specLifecycle is a spec that declared at least one hook. Only this variant satisfies
// Starter/Stopper, which is what keeps a wiring-only capability out of the start list.
type specLifecycle[C any] struct {
	specModule[C]
}

// Start implements Starter.
func (m specLifecycle[C]) Start(ctx context.Context) error {
	if m.spec.Start == nil {
		return nil
	}
	return m.spec.Start(ctx)
}

// Stop implements Stopper. A spec that only declared Stop still gets called here, so a
// capability can clean up what it built during Register.
func (m specLifecycle[C]) Stop(ctx context.Context) error {
	if m.spec.Stop == nil {
		return nil
	}
	return m.spec.Stop(ctx)
}
