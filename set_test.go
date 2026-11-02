package gplugin_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/maxid/gplugin"
)

// errStartFailure is returned by a capability whose Start must fail.
var errStartFailure = errors.New("capability start failure")

// recorder is a minimal container that also records hook order.
type recorder struct {
	mu     sync.Mutex
	values map[string]any
	order  []string
}

func newRecorder() *recorder { return &recorder{values: map[string]any{}} }

func (r *recorder) Set(key string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
}

func (r *recorder) Get(key string) (any, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	return value, ok
}

func (r *recorder) mark(kind, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, kind+":"+id)
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

type recorderKey struct{}

// capability is a test module with an optional lifecycle.
type capability struct {
	id       string
	version  string
	deps     []string
	startErr error
	stopErr  bool
	// stopLog records Stop calls, so rollback order is observable.
	stopLog *recorder
}

func (c *capability) ID() string          { return c.id }
func (c *capability) Version() string     { return c.version }
func (c *capability) DependsOn() []string { return c.deps }

func (c *capability) Register(container *recorder) error {
	container.mark("register", c.id)
	return nil
}

func (c *capability) Start(ctx context.Context) error {
	if rec, ok := ctx.Value(recorderKey{}).(*recorder); ok {
		rec.mark("start", c.id)
	}
	return c.startErr
}

func (c *capability) Stop(context.Context) error {
	if c.stopLog != nil {
		c.stopLog.mark("stop", c.id)
	}
	if c.stopErr {
		return fmt.Errorf("stop %s failed", c.id)
	}
	return nil
}

// wiring is a capability without a lifecycle: it only registers.
type wiring struct{ id string }

func (w wiring) ID() string                         { return w.id }
func (w wiring) Register(container *recorder) error { container.mark("register", w.id); return nil }

func TestAddRejectsWiringErrors(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	if err := set.Add(nil); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("nil module: got %v, want ErrPermanent", err)
	}
	if err := set.Add(wiring{}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("empty id: got %v, want ErrPermanent", err)
	}
	if err := set.Add(wiring{id: "log"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := set.Add(wiring{id: "log"}); !errors.Is(err, gplugin.ErrAlreadyRegistered) {
		t.Fatalf("duplicate: got %v, want ErrAlreadyRegistered", err)
	}
	if got, want := fmt.Sprint(set.IDs()), "[log]"; got != want {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
}

func TestSelectRejectsWiringErrors(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "web", deps: []string{"cache"}})
	mustAdd(t, set, &capability{id: "cache"})
	mustAdd(t, set, &capability{id: "orphan", deps: []string{"nope"}})

	if _, err := set.Select(); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("empty selection: got %v, want ErrPermanent", err)
	}
	if _, err := set.Select("nope"); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("unknown capability: got %v, want ErrPermanent", err)
	}
	if _, err := set.Select("orphan"); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("unregistered dependency: got %v, want ErrPermanent", err)
	}
}

func TestSelectRejectsCycle(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "a", deps: []string{"b"}})
	mustAdd(t, set, &capability{id: "b", deps: []string{"a"}})
	if _, err := set.Select("a"); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("cycle: got %v, want ErrPermanent", err)
	}
}

func TestSelectPullsTransitiveDependenciesInDeterministicOrder(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "web", deps: []string{"cache"}})
	mustAdd(t, set, &capability{id: "cache", deps: []string{"log"}})
	mustAdd(t, set, &capability{id: "log"})
	mustAdd(t, set, &capability{id: "unused"})

	assembly, err := set.Select("web")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got, want := fmt.Sprint(assembly.IDs()), "[log cache web]"; got != want {
		t.Fatalf("order = %v, want %v", got, want)
	}
	// The same declaration must produce the same order every time.
	for i := 0; i < 20; i++ {
		again, err := set.Select("web")
		if err != nil {
			t.Fatalf("select round %d: %v", i, err)
		}
		if got, want := fmt.Sprint(again.IDs()), "[log cache web]"; got != want {
			t.Fatalf("round %d order = %v, want %v", i, got, want)
		}
	}
}

func TestManifestReportsVersionAndDependencies(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "log", version: "1.2.3"})
	mustAdd(t, set, &capability{id: "cache", version: "2.0.0", deps: []string{"log"}})

	assembly, err := set.Select("cache")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	entries := assembly.Manifest()
	if len(entries) != 2 {
		t.Fatalf("manifest has %d entries, want 2", len(entries))
	}
	if entries[0].ID != "log" || entries[0].Version != "1.2.3" {
		t.Fatalf("entry 0 = %+v, want log@1.2.3", entries[0])
	}
	if entries[1].ID != "cache" || entries[1].Version != "2.0.0" || fmt.Sprint(entries[1].DependsOn) != "[log]" {
		t.Fatalf("entry 1 = %+v, want cache@2.0.0 on [log]", entries[1])
	}
}

func TestStartAndStopFollowOrderAndSkipWiringOnly(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "web", deps: []string{"cache"}})
	mustAdd(t, set, &capability{id: "cache"})
	mustAdd(t, set, wiring{id: "wire"})

	assembly, err := set.Select("web", "wire")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	container := newRecorder()
	ctx := context.WithValue(context.Background(), recorderKey{}, container)
	if err := assembly.Start(ctx, container); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got, want := fmt.Sprint(container.events()), "[register:cache register:web register:wire start:cache start:web]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if err := assembly.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestStartFailureRollsBackStartedCapabilitiesOnly(t *testing.T) {
	stops := newRecorder()
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "log", stopLog: stops})
	mustAdd(t, set, &capability{id: "cache", deps: []string{"log"}, startErr: errStartFailure, stopLog: stops})
	mustAdd(t, set, &capability{id: "web", deps: []string{"cache"}, stopLog: stops})

	assembly, err := set.Select("web")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	startErr := assembly.Start(context.Background(), newRecorder())
	if startErr == nil {
		t.Fatal("start: got nil, want the cache failure")
	}
	if !errors.Is(startErr, errStartFailure) {
		t.Fatalf("start error does not wrap the capability failure: %v", startErr)
	}
	// log started and must be stopped; cache never started and web never ran.
	if got, want := fmt.Sprint(stops.events()), "[stop:log]"; got != want {
		t.Fatalf("rollback = %v, want %v", got, want)
	}
}

func TestStopAggregatesFailures(t *testing.T) {
	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, &capability{id: "a", stopErr: true})
	mustAdd(t, set, &capability{id: "b", stopErr: true})
	assembly, err := set.Select("a", "b")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if err := assembly.Start(context.Background(), newRecorder()); err != nil {
		t.Fatalf("start: %v", err)
	}
	stopErr := assembly.Stop(context.Background())
	if stopErr == nil {
		t.Fatal("stop: got nil, want both failures reported")
	}
	for _, want := range []string{"stop a failed", "stop b failed"} {
		if !strings.Contains(stopErr.Error(), want) {
			t.Fatalf("stop error %q does not mention %q", stopErr, want)
		}
	}
}

func TestStaticWrapsIDVersionAndDependencies(t *testing.T) {
	inner := &capability{id: "inner", deps: []string{"log"}}
	wrapped := gplugin.Static("cache", "3.1.4", inner)
	if wrapped.ID() != "cache" {
		t.Fatalf("id = %q, want cache", wrapped.ID())
	}
	versioned, ok := wrapped.(gplugin.Versioned)
	if !ok || versioned.Version() != "3.1.4" {
		t.Fatalf("version = %+v, want 3.1.4", versioned)
	}
	dependent, ok := wrapped.(gplugin.Dependent)
	if !ok || fmt.Sprint(dependent.DependsOn()) != "[log]" {
		t.Fatalf("dependencies = %+v, want [log]", dependent)
	}
	if _, ok := wrapped.(gplugin.Starter); !ok {
		t.Fatal("wrapped module with a lifecycle must satisfy Starter")
	}

	set := gplugin.NewSet[*recorder]()
	mustAdd(t, set, wiring{id: "log"})
	if err := set.Add(wrapped); err != nil {
		t.Fatalf("add: %v", err)
	}
	container := newRecorder()
	if err := wrapped.Register(container); err != nil {
		t.Fatalf("register: %v", err)
	}
	if got, want := fmt.Sprint(container.events()), "[register:inner]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestFromAdaptsForeignModuleShape(t *testing.T) {
	if _, err := gplugin.From(gplugin.Spec[*recorder]{}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("empty id: got %v, want ErrPermanent", err)
	}
	if _, err := gplugin.From(gplugin.Spec[*recorder]{ID: "x"}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("nil register: got %v, want ErrPermanent", err)
	}

	registerCalled := false
	wiringOnly, err := gplugin.From(gplugin.Spec[*recorder]{
		ID:      "cache",
		Version: "1.0.0",
		Register: func(*recorder) error {
			registerCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	if _, ok := wiringOnly.(gplugin.Starter); ok {
		t.Fatal("a spec without hooks must not satisfy Starter")
	}
	if err := wiringOnly.Register(newRecorder()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !registerCalled {
		t.Fatal("register closure was not called")
	}
}

func TestFromCallsOnlyDeclaredHooks(t *testing.T) {
	var events []string
	module, err := gplugin.From(gplugin.Spec[*recorder]{
		ID:        "web",
		DependsOn: []string{"cache"},
		Register:  func(*recorder) error { events = append(events, "register"); return nil },
		Stop:      func(context.Context) error { events = append(events, "stop"); return nil },
	})
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	dependent, ok := module.(gplugin.Dependent)
	if !ok || fmt.Sprint(dependent.DependsOn()) != "[cache]" {
		t.Fatalf("dependencies = %+v, want [cache]", dependent)
	}
	starter, ok := module.(gplugin.Starter)
	if !ok {
		t.Fatal("a spec that declared Stop must satisfy Starter too")
	}
	if err := module.Register(newRecorder()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := starter.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	stopper, ok := module.(gplugin.Stopper)
	if !ok {
		t.Fatal("a spec that declared Stop must satisfy Stopper")
	}
	if err := stopper.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got, want := fmt.Sprint(events), "[register stop]"; got != want {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func mustAdd(t *testing.T, set *gplugin.Set[*recorder], module gplugin.Module[*recorder]) {
	t.Helper()
	if err := set.Add(module); err != nil {
		t.Fatalf("add: %v", err)
	}
}
