English | [简体中文](README.zh-CN.md)

# gplugin

The plugin mechanism shared by the framework and the protocol libraries — two small
layers for two different problems, no third-party dependencies.

```go
type Module[C any] interface {  // assembly: a capability that registers into C
    ID() string
    Register(C) error
}

type Plugin[H any] interface {  // host extension: an extension of a live host H
    Name() string
    Load(H) error
    Unload() error
}
```

`C` and `H` are always types the caller declares. This package knows no protocol, no
framework and no container — which is what lets an application framework and an MQTT
broker share one mechanism without sharing any semantics.

## 1. Assembly — `Set`, `Select`, `Assembly`

An application declares the capabilities it wants; the set resolves the dependency
closure, orders it topologically and reports a manifest.

```go
set := gplugin.NewSet[*gable.Container]()
set.Add(logCapability)                                                     // id "log"
set.Add(gplugin.Static("cache", "1.0.0", cacheCapability))                  // DependsOn: log
set.Add(gplugin.Static("web", "1.0.0", webCapability))                      // DependsOn: cache

assembly, err := set.Select("web")   // resolves log -> cache -> web
if err != nil {
    return err                       // unknown capability, missing dep, cycle
}
defer assembly.Stop(ctx)             // reverse order, failures joined
if err := assembly.Start(ctx, container); err != nil {
    return err                       // already-started capabilities were rolled back
}
for _, entry := range assembly.Manifest() {
    log.Printf("capability %s@%s needs %v", entry.ID, entry.Version, entry.DependsOn)
}
```

Guarantees:

- **Deterministic order.** Dependencies first; ties break on id, so every process start
  wires the same order and a rolling restart cannot reorder anything.
- **Failures are startup errors.** An unknown capability, an unselected dependency and a
  cycle are reported, never silently dropped from the assembly.
- **No package-level registry, no panic.** Wiring is explicit and testable in isolation;
  a duplicate id is an error the caller sees.
- **Optional lifecycle.** A wiring-only capability does not implement `Start`/`Stop`;
  it is not listed as started and a rollback has nothing to stop for it.

`From(Spec[C])` adapts a module whose own `Register` takes a concrete container type or
that has its own lifecycle interface, without renaming anything in its repository.

## 2. Host extensions — `Plugin`, `Registry`

A library owns a host object and lets named extensions attach to it.

```go
reg := gplugin.NewRegistry[*Broker]()
reg.Add(authPlugin)                 // Name() "auth"
reg.Add(metricsPlugin)              // Name() "metrics", DependsOn: []string{"auth"}

entries, err := reg.Manifest()      // planned load order, without loading anything
if err := reg.Load(broker); err != nil {
    return err                      // plugins loaded before the failure were unloaded
}
defer reg.Unload()                  // reverse load order, failures joined
```

Guarantees: duplicate names are errors; a failing `Load` unloads what already loaded;
`Unload` runs newest first and joins every failure; loading twice is refused (it would
register hooks twice).

## 3. What this package does not do

- **No dynamic plugins.** No `.so`, no download, no service discovery. A plugin is
  compiled into the binary; this package decides what is wired and in which order, not
  what exists at run time.
- **No protocol semantics.** Hooks, events and lifecycles stay in the library that owns
  them.
- **No global state.** Nothing is registered at package initialization.

## 4. Naming

`gplugin` sits below both the framework layer and the protocol libraries: it may be
imported by any of them and depends on none of them. See the dependency rules in the
consuming repository.

## Version

`Version = "0.0.0-dev"` — pre-1.0, no promises of API stability yet.

## License

MIT, see [LICENSE](LICENSE).
