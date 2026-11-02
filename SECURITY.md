English | [简体中文](SECURITY.zh-CN.md)

# Security policy

## Supported versions

Pre-1.0 (`Version = "0.0.0-dev"`): only the tip of `main` receives fixes.

## Reporting a vulnerability

Do not open a public issue. Report privately to the maintainers (security contact listed
on the repository profile) with a description, the affected version or commit, and a
minimal reproduction. You will receive an acknowledgement and a fix timeline.

## Scope

This library is a wiring mechanism, so its security surface is small and structural:

- **No dynamic loading.** A plugin must be compiled into the binary; there is no path for
  a remote or on-disk artifact to become code.
- **No global state.** Nothing is registered at package initialization, so importing the
  package cannot change another process's wiring.
- **Failures are loud.** A duplicate name or id, an unknown dependency and a cycle are
  errors; a partially assembled or partially loaded set never keeps running silently.

Anything that breaks one of these three statements is a security-relevant bug.
