English | [简体中文](ROADMAP.zh-CN.md)

# Roadmap

The library is a mechanism, not a product: it grows only when a consumer needs it.

## Now (0.0.0-dev)

- [x] Assembly layer: selection, dependency closure, deterministic order, manifest,
      optional lifecycle, rollback.
- [x] Host layer: named plugins, dependency order, load rollback, reverse unload.
- [x] Adapters for foreign module shapes (`Static`, `Spec`/`From`).

## Next

- [ ] Chain several contributions to one hook (needs a consumer with that requirement).
- [ ] Per-plugin configuration decoding, if a consumer wants declarative plugin config.

## Not planned

- Dynamic loading (`.so`, download, service discovery): a plugin is compiled in.
- Protocol or framework semantics: those stay in the library that owns them.
