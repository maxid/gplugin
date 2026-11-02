English | [简体中文](CHANGELOG.zh-CN.md)

# Changelog

All notable changes to this library are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow the policy in the
consuming repository (pre-1.0 while `Version = "0.0.0-dev"`).

## [Unreleased]

### Added

- Assembly layer: `Set`, `Select`, `Assembly`, `Module[C]`, optional
  `Versioned`/`Dependent`/`Starter`/`Stopper`, `Static`, `Spec[C]`/`From`.
- Host layer: `Plugin[H]`, `Registry[H]` with deterministic load order, rollback on a
  failed load, reverse-order unload and manifest reporting.
