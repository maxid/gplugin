// Command lifecycle shows what an application does with gplugin's lifecycle hooks: start
// a few plugins, observe every transition, and print a status table whose light is grey
// (registered), green (enabled) or red (stopped).
//
// The colour lives here, not in the library: gplugin reports a State, and this program
// decides that a terminal should render it as a colour. Set NO_COLOR=1 for plain output,
// which is what a log file or a CI job wants.
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/maxid/gplugin"
)

// host is the service object the plugins attach to. A plugin keeps a reference to it and
// installs its callbacks; here it just records what happened.
type host struct {
	mu     sync.Mutex
	events []string
}

func (h *host) publish(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *host) drain() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	events := h.events
	h.events = nil
	return events
}

// plugin is a small extension: it subscribes to one topic family of the host.
type plugin struct {
	name string
	deps []string
	// fail makes Start return an error, to show how a failure is reported.
	fail bool
}

func (p *plugin) Name() string        { return p.name }
func (p *plugin) DependsOn() []string { return p.deps }
func (p *plugin) Start(h *host) error {
	if p.fail {
		return fmt.Errorf("%s: upstream unreachable", p.name)
	}
	h.publish("subscribed " + p.name)
	return nil
}

func (p *plugin) Stop() error { return nil }

// observer prints every transition as it happens. This is the extension point: the
// application decides, the library only reports.
type observer struct{}

func (observer) StateChanged(event gplugin.Event) {
	if event.Err != nil {
		fmt.Printf("  ! %-8s %s -> %s: %v\n", event.ID, event.From, event.To, event.Err)
		return
	}
	fmt.Printf("  · %-8s %s -> %s\n", event.ID, event.From, event.To)
}

func main() {
	registry := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observer{}))
	for _, p := range []*plugin{
		{name: "auth"},
		{name: "metrics", deps: []string{"auth"}},
		{name: "telemetry", deps: []string{"metrics"}},
		{name: "audit", deps: []string{"telemetry"}},
		{name: "replay", fail: true},
	} {
		if err := registry.Add(p); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	fmt.Println("startup: start the plugins this process wires in")
	if err := registry.Start("auth", "metrics", "telemetry", "audit"); err != nil {
		fmt.Println("  start failed:", err)
	}
	fmt.Println("\nafter startup:")
	printStatus(registry)

	fmt.Println("\nstartup: a broken plugin is reported, not silently skipped")
	if err := registry.Start("replay"); err != nil {
		fmt.Println("  start failed:", err)
	}
	printStatus(registry)

	fmt.Println("\nruntime: switch telemetry off — its dependents go with it")
	if err := registry.Stop("telemetry"); err != nil {
		fmt.Println("  stop failed:", err)
	}
	printStatus(registry)

	fmt.Println("\nruntime: switch audit back on — its dependencies start first")
	if err := registry.Start("audit"); err != nil {
		fmt.Println("  start failed:", err)
	}
	printStatus(registry)
}

// printStatus is the "what is enabled" report: one line per plugin, in start order.
func printStatus(registry *gplugin.Registry[*host]) {
	statuses, err := registry.Snapshot()
	if err != nil {
		fmt.Println("  snapshot failed:", err)
		return
	}
	for _, status := range statuses {
		line := fmt.Sprintf("  %s %-8s %-10s", light(status.State), status.ID, status.State)
		if len(status.DependsOn) > 0 {
			line += " needs " + strings.Join(status.DependsOn, ",")
		}
		if status.Err != nil {
			line += "  last error: " + status.Err.Error()
		}
		fmt.Println(line)
	}
}

// light maps a state to the operator's mental model: grey = known but not running,
// green = running, red = switched off. Plain markers when NO_COLOR is set.
func light(state gplugin.State) string {
	color := os.Getenv("NO_COLOR") == ""
	switch state {
	case gplugin.StateEnabled:
		if color {
			return "\x1b[32m●\x1b[0m" // green
		}
		return "(+)"
	case gplugin.StateStopped:
		if color {
			return "\x1b[31m●\x1b[0m" // red
		}
		return "(-)"
	default:
		if color {
			return "\x1b[90m●\x1b[0m" // grey
		}
		return "( )"
	}
}
