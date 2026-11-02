package gplugin_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/maxid/gplugin"
)

// host is a stand-in for a library's own service object.
type host struct {
	log []string
}

func (h *host) mark(event string) { h.log = append(h.log, event) }

// fakePlugin records start and stop calls, and can fail on demand.
type fakePlugin struct {
	name     string
	deps     []string
	version  string
	startErr error
	stopErr  error
}

func (p *fakePlugin) Name() string        { return p.name }
func (p *fakePlugin) DependsOn() []string { return p.deps }
func (p *fakePlugin) Version() string     { return p.version }
func (p *fakePlugin) Start(h *host) error {
	if p.startErr != nil {
		return p.startErr
	}
	h.mark("start:" + p.name)
	return nil
}
func (p *fakePlugin) Stop() error {
	if p.stopErr != nil {
		return p.stopErr
	}
	return nil
}

// loaderOnly is a plugin with no dependencies and no version.
type loaderOnly struct {
	name   string
	onLoad func()
}

func (p *loaderOnly) Name() string { return p.name }
func (p *loaderOnly) Start(*host) error {
	if p.onLoad != nil {
		p.onLoad()
	}
	return nil
}
func (p *loaderOnly) Stop() error { return nil }

// stateLog collects observed transitions.
type stateLog struct {
	received []gplugin.Event
}

func (l *stateLog) StateChanged(event gplugin.Event) { l.received = append(l.received, event) }

func (l *stateLog) transitions() []string {
	out := make([]string, 0, len(l.received))
	for _, event := range l.received {
		line := fmt.Sprintf("%s:%s->%s", event.ID, event.From, event.To)
		if event.Err != nil {
			line += "!" + event.Err.Error()
		}
		out = append(out, line)
	}
	return out
}

func TestRegistryAddRejectsWiringErrors(t *testing.T) {
	reg := gplugin.NewRegistry(&host{})
	if err := reg.Add(nil); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("nil plugin: got %v, want ErrPermanent", err)
	}
	if err := reg.Add(&loaderOnly{}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("empty name: got %v, want ErrPermanent", err)
	}
	if err := reg.Add(&loaderOnly{name: "auth"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := reg.Add(&loaderOnly{name: "auth"}); !errors.Is(err, gplugin.ErrAlreadyRegistered) {
		t.Fatalf("duplicate: got %v, want ErrAlreadyRegistered", err)
	}
	if got, want := fmt.Sprint(reg.Names()), "[auth]"; got != want {
		t.Fatalf("names = %v, want %v", got, want)
	}
	if state, ok := reg.State("auth"); !ok || state != gplugin.StateRegistered {
		t.Fatalf("state = %v/%v, want registered", state, ok)
	}
}

func TestRegistryStartsInDependencyOrder(t *testing.T) {
	observers := &stateLog{}
	reg := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observers))
	mustAddPlugin(t, reg, &fakePlugin{name: "metrics"})
	mustAddPlugin(t, reg, &fakePlugin{name: "telemetry", deps: []string{"metrics"}, version: "1.0.0"})
	mustAddPlugin(t, reg, &fakePlugin{name: "audit", deps: []string{"telemetry"}})

	statuses, err := reg.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got, want := fmt.Sprint(statusIDs(statuses)), "[metrics telemetry audit]"; got != want {
		t.Fatalf("snapshot order = %v, want %v", got, want)
	}
	if statuses[1].Version != "1.0.0" || fmt.Sprint(statuses[2].DependsOn) != "[telemetry]" {
		t.Fatalf("snapshot rows = %+v", statuses)
	}
	if statuses[0].State != gplugin.StateRegistered {
		t.Fatalf("snapshot state = %v, want registered", statuses[0].State)
	}
	if len(observers.received) != 0 {
		t.Fatalf("snapshot emitted %d events, want 0", len(observers.received))
	}

	if err := reg.StartAll(); err != nil {
		t.Fatalf("start all: %v", err)
	}
	if got, want := fmt.Sprint(reg.Enabled()), "[metrics telemetry audit]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	if got, want := fmt.Sprint(observers.transitions()), "[metrics:registered->enabled telemetry:registered->enabled audit:registered->enabled]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestRegistryRejectsUnknownDependencyAndCycle(t *testing.T) {
	reg := gplugin.NewRegistry(&host{})
	mustAddPlugin(t, reg, &fakePlugin{name: "audit", deps: []string{"missing"}})
	if _, err := reg.Snapshot(); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("unknown dependency: got %v, want ErrPermanent", err)
	}

	cyclic := gplugin.NewRegistry(&host{})
	mustAddPlugin(t, cyclic, &fakePlugin{name: "a", deps: []string{"b"}})
	mustAddPlugin(t, cyclic, &fakePlugin{name: "b", deps: []string{"a"}})
	if err := cyclic.StartAll(); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("cycle: got %v, want ErrPermanent", err)
	}
}

func TestStartPullsDependenciesAndIsIdempotent(t *testing.T) {
	observers := &stateLog{}
	reg := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observers))
	mustAddPlugin(t, reg, &fakePlugin{name: "log"})
	mustAddPlugin(t, reg, &fakePlugin{name: "cache", deps: []string{"log"}})
	mustAddPlugin(t, reg, &fakePlugin{name: "web", deps: []string{"cache"}})

	if err := reg.Start("web"); err != nil {
		t.Fatalf("start web: %v", err)
	}
	if got, want := fmt.Sprint(reg.Enabled()), "[log cache web]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	if got, want := len(observers.received), 3; got != want {
		t.Fatalf("events = %d, want %d", got, want)
	}

	// Starting again is a no-op: no new event, still enabled.
	if err := reg.Start("web"); err != nil {
		t.Fatalf("restart web: %v", err)
	}
	if got, want := len(observers.received), 3; got != want {
		t.Fatalf("events after restart = %d, want %d", got, want)
	}
}

func TestStopCascadesToDependents(t *testing.T) {
	observers := &stateLog{}
	reg := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observers))
	mustAddPlugin(t, reg, &fakePlugin{name: "log"})
	mustAddPlugin(t, reg, &fakePlugin{name: "cache", deps: []string{"log"}})
	mustAddPlugin(t, reg, &fakePlugin{name: "web", deps: []string{"cache"}})
	mustAddPlugin(t, reg, &fakePlugin{name: "unrelated"})
	if err := reg.StartAll(); err != nil {
		t.Fatalf("start all: %v", err)
	}
	observers.received = nil

	if err := reg.Stop("cache"); err != nil {
		t.Fatalf("stop cache: %v", err)
	}
	// cache and everything that depends on it, newest first; log and unrelated stay.
	if got, want := fmt.Sprint(reg.Enabled()), "[log unrelated]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	if got, want := fmt.Sprint(observers.transitions()), "[web:enabled->stopped cache:enabled->stopped]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if state, _ := reg.State("web"); state != gplugin.StateStopped {
		t.Fatalf("web state = %v, want stopped", state)
	}
	// Stopping something that is not running is a no-op.
	if err := reg.Stop("web"); err != nil {
		t.Fatalf("stop web again: %v", err)
	}
}

func TestStartFailureRollsBackOnlyWhatItStarted(t *testing.T) {
	observers := &stateLog{}
	reg := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observers))
	mustAddPlugin(t, reg, &fakePlugin{name: "log"})
	mustAddPlugin(t, reg, &fakePlugin{name: "cache", deps: []string{"log"}, startErr: errStartFailure})
	mustAddPlugin(t, reg, &fakePlugin{name: "web", deps: []string{"cache"}})

	if err := reg.Start("log"); err != nil {
		t.Fatalf("start log: %v", err)
	}
	observers.received = nil

	err := reg.Start("web")
	if !errors.Is(err, errStartFailure) {
		t.Fatalf("start web: got %v, want the cache failure", err)
	}
	if !strings.Contains(err.Error(), "cache") {
		t.Fatalf("error %q does not name the failing plugin", err)
	}
	// log was already running before this call, so it must not be rolled back.
	if got, want := fmt.Sprint(reg.Enabled()), "[log]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	// The failure is observed once, and it does not change the state.
	if got, want := fmt.Sprint(observers.transitions()), "[cache:registered->registered!capability start failure]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
	statuses, _ := reg.Snapshot()
	for _, status := range statuses {
		if status.ID == "cache" && !errors.Is(status.Err, errStartFailure) {
			t.Fatalf("cache status err = %v, want the start failure", status.Err)
		}
	}
	// A later successful start clears the recorded failure.
	if err := reg.Add(&fakePlugin{name: "cache2"}); err != nil {
		t.Fatalf("add: %v", err)
	}
}

func TestStartFailureRollsBackNewlyStartedDependencies(t *testing.T) {
	reg := gplugin.NewRegistry(&host{})
	mustAddPlugin(t, reg, &fakePlugin{name: "log"})
	mustAddPlugin(t, reg, &fakePlugin{name: "cache", deps: []string{"log"}, startErr: errStartFailure})

	if err := reg.Start("cache"); !errors.Is(err, errStartFailure) {
		t.Fatalf("start cache: got %v, want the cache failure", err)
	}
	// log was started by this call to satisfy the dependency, so the rollback stopped it.
	if got, want := fmt.Sprint(reg.Enabled()), "[]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	if state, _ := reg.State("log"); state != gplugin.StateStopped {
		t.Fatalf("log state = %v, want stopped", state)
	}
}

func TestStopFailureKeepsGoingAndJoinsErrors(t *testing.T) {
	reg := gplugin.NewRegistry(&host{})
	mustAddPlugin(t, reg, &fakePlugin{name: "a", stopErr: errStartFailure})
	mustAddPlugin(t, reg, &fakePlugin{name: "b", deps: []string{"a"}, stopErr: errStartFailure})
	if err := reg.StartAll(); err != nil {
		t.Fatalf("start all: %v", err)
	}
	err := reg.StopAll()
	if err == nil {
		t.Fatal("stop all: got nil, want both failures reported")
	}
	for _, want := range []string{"stop a", "stop b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("stop error %q does not mention %q", err, want)
		}
	}
	// A plugin whose Stop failed did not confirm it stopped: it stays enabled with the
	// reason recorded, so a status view shows the problem instead of hiding it.
	if got, want := fmt.Sprint(reg.Enabled()), "[a b]"; got != want {
		t.Fatalf("enabled = %v, want %v", got, want)
	}
	statuses, _ := reg.Snapshot()
	for _, status := range statuses {
		if status.State != gplugin.StateEnabled || status.Err == nil {
			t.Fatalf("status %s = %v/%v, want enabled with an error", status.ID, status.State, status.Err)
		}
	}
}

func TestStartAndStopRejectUnknownNames(t *testing.T) {
	reg := gplugin.NewRegistry(&host{})
	if err := reg.Start(); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("start with no name: got %v, want ErrPermanent", err)
	}
	if err := reg.Start("nope"); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("start unknown: got %v, want ErrPermanent", err)
	}
	if err := reg.Stop("nope"); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("stop unknown: got %v, want ErrPermanent", err)
	}
	if err := reg.StartAll(); err != nil {
		t.Fatalf("start all on an empty registry: %v", err)
	}
	if err := reg.StopAll(); err != nil {
		t.Fatalf("stop all on an empty registry: %v", err)
	}
}

func TestRestartAfterStopTransitionsAgain(t *testing.T) {
	observers := &stateLog{}
	reg := gplugin.NewRegistry(&host{}, gplugin.WithObserver(observers))
	mustAddPlugin(t, reg, &loaderOnly{name: "auth"})
	if err := reg.Start("auth"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := reg.Stop("auth"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := reg.Start("auth"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if got, want := fmt.Sprint(observers.transitions()), "[auth:registered->enabled auth:enabled->stopped auth:stopped->enabled]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func statusIDs(statuses []gplugin.Status) []string {
	ids := make([]string, 0, len(statuses))
	for _, status := range statuses {
		ids = append(ids, status.ID)
	}
	return ids
}

func mustAddPlugin(t *testing.T, reg *gplugin.Registry[*host], plugin gplugin.Plugin[*host]) {
	t.Helper()
	if err := reg.Add(plugin); err != nil {
		t.Fatalf("add: %v", err)
	}
}
