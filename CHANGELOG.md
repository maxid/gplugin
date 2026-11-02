English | [简体中文](CHANGELOG.zh-CN.md)

# Changelog

All notable changes to this library are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow the policy in the
consuming repository (pre-1.0 while `Version = "0.0.0-dev"`).

## [Unreleased]

### Added

- Assembly layer: `Set`, `Select`, `Assembly`, `Module[C]`, optional
  `Versioned`/`Dependent`/`Starter`/`Stopper`, `Static`, `Spec[C]`/`From`.
- Host layer: `Plugin[H]`, `Registry[H]` with deterministic start order, rollback on a
  failed start, cascading stop and status reporting.
- Lifecycle hooks: `State` (`StateRegistered`/`StateEnabled`/`StateStopped`), `Status`,
  `Event` and `Observer`, registered with `WithObserver`; `Snapshot()` reports every
  plugin with its state and last failure without side effects.
- Per-plugin control: `Start`/`Stop` (dependency closure, cascading stop, idempotent) and
  `StartAll`/`StopAll`; `Enabled()` lists what is running.
- `examples/lifecycle`: a runnable status table whose light is grey (registered), green
  (enabled) or red (stopped) — the colour lives in the application.

### Changed

- **Breaking, no consumers yet:** `Plugin.Load`/`Unload` are now `Start(H)`/`Stop()`, and
  `Registry.Load`/`Unload`/`Loaded` are now `Start`/`Stop`/`Enabled` plus `StartAll`/
  `StopAll`. Starting is loading and stopping is unloading; one vocabulary from the library
  to the operator.
- `Registry` is bound to its host at construction (`NewRegistry(host, opts...)`), since a
  plugin can start long after boot and must receive the same host.
