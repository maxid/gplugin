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
    Start(H) error
    Stop() error
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

A library owns a host object and lets named extensions attach to it — and switch on and
off at run time.

```go
reg := gplugin.NewRegistry(broker, gplugin.WithObserver(myStatusLight{}))
reg.Add(authPlugin)                 // Name() "auth"
reg.Add(metricsPlugin)              // Name() "metrics", DependsOn: []string{"auth"}

if err := reg.Start("metrics"); err != nil {   // starts auth first
    return err                                 // auth/metrics were rolled back
}
defer reg.StopAll()                            // newest first, failures joined

statuses, err := reg.Snapshot()     // what is wired in, in start order
for _, s := range statuses {
    log.Printf("%s %s %s", s.ID, s.State, s.Err)
}
```

**States.** `StateRegistered` (known, never started), `StateEnabled` (running) and
`StateStopped` (ran, then stopped). The three are deliberately distinct: "switched off" and
"never switched on" are different facts, and an operator needs both.

**Observers.** `Observer{StateChanged(Event)}` receives every transition with its `From`,
`To` and, on a failure, the `Err` — so an application can print what started, light a
status indicator grey/green/red, or count failures, without polling. Observers run
synchronously after the transition is recorded, with no lock held: they must not block and
must not call back into `Start`/`Stop`.

**Guarantees.**

- **Order is dependency order**, ties break on name, so a rolling restart cannot reorder.
- **Start pulls its closure.** `Start("web")` starts the dependencies too, and skips the
  ones already running, so a control call is retryable.
- **Stop cascades.** `Stop("cache")` stops everything that depends on `cache`, newest
  first; a plugin whose `Stop` failed stays enabled with the reason recorded rather than
  pretending it stopped.
- **Failures are visible.** A failed `Start` rolls back only what that call started,
  records the reason, emits an event carrying it and returns an error naming the plugin.
- Duplicate names, empty names and unregistered names are errors; nothing is silently
  smaller.

See [`examples/lifecycle`](examples/lifecycle/main.go) for a runnable status table whose
light is grey, green or red — the colour lives in the application, not here.

## 3. What this package does not do

- **No dynamic plugins.** No `.so`, no download, no service discovery. A plugin is
  compiled into the binary; this package decides what is wired and in which order, not
  what exists at run time.
- **No protocol semantics.** Hooks, events and lifecycles stay in the library that owns
  them.
- **No presentation.** This package reports a `State`; whether it becomes a colour, a word
  or a metric is the application's decision.
- **No global state.** Nothing is registered at package initialization.

## 4. Naming

`gplugin` sits below both the framework layer and the protocol libraries: it may be
imported by any of them and depends on none of them. See the dependency rules in the
consuming repository.

## Version

`Version = "0.0.0-dev"` — pre-1.0, no promises of API stability yet.

## License

MIT, see [LICENSE](LICENSE).
