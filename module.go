package gplugin

import "context"

// Module is an assembled capability: it registers its values into the container and may
// have a lifecycle.
//
// C is the container type of the caller. An application framework passes its own
// container type; a test passes a map; this package never assumes either.
type Module[C any] interface {
	ID() string
	Register(C) error
}

// Versioned reports a module's or plugin's version, shown in manifests.
//
// It is optional so an existing type keeps working: an unversioned module reports an
// empty version, which is honest — nothing is ever invented.
type Versioned interface {
	Version() string
}

// Dependent declares the ids that must come first, for both layers of this package.
type Dependent interface {
	DependsOn() []string
}

// Starter is the optional lifecycle hook of a Module.
type Starter interface {
	Start(context.Context) error
}

// Stopper is the reverse-order shutdown hook of a Module.
type Stopper interface {
	Stop(context.Context) error
}

// Entry is one row of a manifest: what is wired, at which version, on what it depends.
type Entry struct {
	ID        string
	Version   string
	DependsOn []string
}

// entryOf builds the manifest row of anything that has an id.
func entryOf(id string, value any) Entry {
	return Entry{ID: id, Version: versionOf(value), DependsOn: dependenciesOf(value)}
}

// versionOf reads an optional Versioned; an unversioned entry reports "".
func versionOf(value any) string {
	if versioned, ok := value.(Versioned); ok {
		return versioned.Version()
	}
	return ""
}

// dependenciesOf reads an optional Dependent and copies the list, so a caller cannot
// mutate the plugin's own declaration.
func dependenciesOf(value any) []string {
	if dependent, ok := value.(Dependent); ok {
		return append([]string(nil), dependent.DependsOn()...)
	}
	return nil
}
