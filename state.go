package gplugin

// State is the lifecycle position of one entry.
//
// The three values are what an operator needs to tell apart: known but never started,
// running, and stopped again. An application renders them however it likes — a colour, a
// word, a metric — because presentation is the application's decision, not this package's.
type State int

const (
	// StateRegistered means the entry is known and has not started yet.
	StateRegistered State = iota
	// StateEnabled means the entry is running.
	StateEnabled
	// StateStopped means the entry ran and was stopped. It is deliberately distinct from
	// StateRegistered: "switched off" and "never switched on" are different facts.
	StateStopped
)

// String returns the state as a lower-case word, for logs and status output.
func (s State) String() string {
	switch s {
	case StateRegistered:
		return "registered"
	case StateEnabled:
		return "enabled"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Status is one row of a snapshot: what is registered, and where it stands.
type Status struct {
	ID        string
	Version   string
	DependsOn []string
	// State is the current position.
	State State
	// Err is the last start or stop failure, nil after a clean transition. It never
	// changes State: a plugin whose start failed is still not running, and pretending
	// otherwise would hide the reason.
	Err error
}

// Event is one observed state transition.
//
// A failed transition reports From == To with Err set, so an observer can log the failure
// from the event alone instead of querying the registry again.
type Event struct {
	ID   string
	From State
	To   State
	Err  error
}

// Observer receives every state transition.
//
// It is how an application learns what happened without polling: printing what started,
// turning a status light grey, green or red, counting failures. Observers are called
// synchronously, after the transition is already recorded and with no lock held; an
// implementation must not block and must not call back into the registry's Start or Stop.
type Observer interface {
	StateChanged(Event)
}

// Option configures a Registry.
type Option func(*options)

type options struct {
	observers []Observer
}

// WithObserver registers an observer. Passing it several times registers several.
func WithObserver(observer Observer) Option {
	return func(o *options) {
		if observer != nil {
			o.observers = append(o.observers, observer)
		}
	}
}
