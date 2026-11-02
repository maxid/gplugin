// Package gplugin is the plugin mechanism shared by the framework and the protocol
// libraries.
//
// It answers two different questions with two small layers, because they are two
// different problems:
//
//   - Assembly (Set / Select / Assembly): an application declares the capabilities it
//     wants ("log", "cache", "web"), the set resolves the dependency closure, orders it
//     topologically and produces a manifest. Capabilities register values into a
//     container of the caller's type C.
//   - Host extensions (Plugin / Registry): a library owns a host object H (a broker, a
//     server) and lets named plugins attach to it. A plugin starts against the host,
//     stops in reverse order, and reports a state — registered, enabled or stopped — that
//     an Observer can turn into whatever the application needs: a log line, a status
//     light, a metric.
//
// Neither layer knows any protocol, framework or container: C and H are always types the
// caller declares. That is what lets an application framework and an MQTT broker share
// one mechanism without sharing any semantics.
//
// Deliberately not supported: dynamic plugins (no .so, no download, no service
// discovery). A plugin is compiled into the binary; the mechanism decides what is wired,
// in which order, and whether it is currently switched on — not what exists at run time.
package gplugin
