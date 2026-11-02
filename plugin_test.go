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

// fakePlugin records load and unload calls, and can fail on demand.
type fakePlugin struct {
	name      string
	deps      []string
	version   string
	loadErr   error
	unloadErr error
}

func (p *fakePlugin) Name() string        { return p.name }
func (p *fakePlugin) DependsOn() []string { return p.deps }
func (p *fakePlugin) Version() string     { return p.version }
func (p *fakePlugin) Load(h *host) error {
	if p.loadErr != nil {
		return p.loadErr
	}
	h.mark("load:" + p.name)
	return nil
}
func (p *fakePlugin) Unload() error {
	if p.unloadErr != nil {
		return p.unloadErr
	}
	return nil
}

// loaderOnly is a plugin with no dependencies and no version.
type loaderOnly struct {
	name   string
	onLoad func()
}

func (p *loaderOnly) Name() string { return p.name }
func (p *loaderOnly) Load(*host) error {
	if p.onLoad != nil {
		p.onLoad()
	}
	return nil
}
func (p *loaderOnly) Unload() error { return nil }

func TestRegistryAddRejectsWiringErrors(t *testing.T) {
	reg := gplugin.NewRegistry[*host]()
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
}

func TestRegistryLoadsInDependencyOrder(t *testing.T) {
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &fakePlugin{name: "metrics"})
	mustAddPlugin(t, reg, &fakePlugin{name: "telemetry", deps: []string{"metrics"}, version: "1.0.0"})
	mustAddPlugin(t, reg, &fakePlugin{name: "audit", deps: []string{"telemetry"}})

	entries, err := reg.Manifest()
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if got, want := fmt.Sprint(pluginIDs(entries)), "[metrics telemetry audit]"; got != want {
		t.Fatalf("manifest order = %v, want %v", got, want)
	}
	if entries[1].Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", entries[1].Version)
	}
	if fmt.Sprint(entries[2].DependsOn) != "[telemetry]" {
		t.Fatalf("dependencies = %v, want [telemetry]", entries[2].DependsOn)
	}

	h := &host{}
	if err := reg.Load(h); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := fmt.Sprint(h.log), "[load:metrics load:telemetry load:audit]"; got != want {
		t.Fatalf("load order = %v, want %v", got, want)
	}
	if got, want := fmt.Sprint(reg.Loaded()), "[metrics telemetry audit]"; got != want {
		t.Fatalf("loaded = %v, want %v", got, want)
	}
}

func TestRegistryRejectsUnknownDependencyAndCycle(t *testing.T) {
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &fakePlugin{name: "audit", deps: []string{"missing"}})
	if _, err := reg.Manifest(); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("unknown dependency: got %v, want ErrPermanent", err)
	}

	cyclic := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, cyclic, &fakePlugin{name: "a", deps: []string{"b"}})
	mustAddPlugin(t, cyclic, &fakePlugin{name: "b", deps: []string{"a"}})
	if err := cyclic.Load(&host{}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("cycle: got %v, want ErrPermanent", err)
	}
}

func TestRegistryLoadFailureUnloadsAlreadyLoaded(t *testing.T) {
	var unloaded []string
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &fakePlugin{name: "first"})
	mustAddPlugin(t, reg, &loaderOnly{name: "second", onLoad: func() { unloaded = append(unloaded, "loaded-second") }})
	mustAddPlugin(t, reg, &fakePlugin{name: "third", loadErr: errStartFailure})

	err := reg.Load(&host{})
	if err == nil {
		t.Fatal("load: got nil, want the third plugin's failure")
	}
	if !errors.Is(err, errStartFailure) {
		t.Fatalf("load error does not wrap the plugin failure: %v", err)
	}
	if !strings.Contains(err.Error(), "third") {
		t.Fatalf("load error %q does not name the failing plugin", err)
	}
	if got, want := fmt.Sprint(unloaded), "[loaded-second]"; got != want {
		t.Fatalf("rollback = %v, want %v", got, want)
	}
	if got := fmt.Sprint(reg.Loaded()); got != "[]" {
		t.Fatalf("loaded after rollback = %v, want []", got)
	}
}

func TestRegistryLoadIsNotRepeatable(t *testing.T) {
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &loaderOnly{name: "auth"})
	if err := reg.Load(&host{}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := reg.Load(&host{}); !errors.Is(err, gplugin.ErrPermanent) {
		t.Fatalf("second load: got %v, want ErrPermanent", err)
	}
}

func TestRegistryUnloadsInReverseAndAggregatesFailures(t *testing.T) {
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &fakePlugin{name: "a", unloadErr: errStartFailure})
	mustAddPlugin(t, reg, &fakePlugin{name: "b", unloadErr: errStartFailure})
	if err := reg.Load(&host{}); err != nil {
		t.Fatalf("load: %v", err)
	}
	err := reg.Unload()
	if err == nil {
		t.Fatal("unload: got nil, want both failures reported")
	}
	for _, want := range []string{"unload a", "unload b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("unload error %q does not mention %q", err, want)
		}
	}
	if got := len(reg.Loaded()); got != 0 {
		t.Fatalf("loaded after unload = %d, want 0", got)
	}
	if err := reg.Unload(); err != nil {
		t.Fatalf("unload when empty: %v", err)
	}
}

func TestRegistryLoadsAgainAfterUnload(t *testing.T) {
	loads := 0
	reg := gplugin.NewRegistry[*host]()
	mustAddPlugin(t, reg, &loaderOnly{name: "auth", onLoad: func() { loads++ }})
	if err := reg.Load(&host{}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := reg.Unload(); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if err := reg.Load(&host{}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loads != 2 {
		t.Fatalf("loads = %d, want 2", loads)
	}
}

func pluginIDs(entries []gplugin.Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}

func mustAddPlugin(t *testing.T, reg *gplugin.Registry[*host], plugin gplugin.Plugin[*host]) {
	t.Helper()
	if err := reg.Add(plugin); err != nil {
		t.Fatalf("add: %v", err)
	}
}
