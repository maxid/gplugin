package gplugin

import (
	"errors"
	"fmt"
)

// ErrPermanent marks wiring mistakes: retrying cannot help.
var ErrPermanent = errors.New("gplugin: permanent error")

// ErrAlreadyRegistered means an id or name was registered twice.
var ErrAlreadyRegistered = errors.New("gplugin: already registered")

// joinErrors aggregates failures without dropping any.
func joinErrors(failures []error) error {
	switch len(failures) {
	case 0:
		return nil
	case 1:
		return failures[0]
	}
	joined := make([]error, 0, len(failures)+1)
	joined = append(joined, fmt.Errorf("%d failures:", len(failures)))
	joined = append(joined, failures...)
	return errors.Join(joined...)
}
