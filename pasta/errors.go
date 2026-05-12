package pasta

import "errors"

var (
	// ErrClosed reports that a workspace mutation was attempted after Close.
	ErrClosed = errors.New("pasta: workspace is closed")
	// ErrDuplicate reports that an object with the same identity already exists.
	ErrDuplicate = errors.New("pasta: duplicate object")
	// ErrInvalidName reports that a library, class, or type name is malformed.
	ErrInvalidName = errors.New("pasta: invalid name")
	// ErrInvalidID reports that an ID or composed object name is malformed.
	ErrInvalidID = errors.New("pasta: invalid id")
	// ErrNotFound reports that a referenced object does not exist.
	ErrNotFound = errors.New("pasta: object not found")
	// ErrOwnership reports that a scoped caller attempted to mutate an object it does not own.
	ErrOwnership = errors.New("pasta: ownership violation")
	// ErrInvalidPort reports that a referenced port is missing or has the wrong direction.
	ErrInvalidPort = errors.New("pasta: invalid port")
	// ErrTypeMismatch reports that a link type is not accepted by one of its endpoints.
	ErrTypeMismatch = errors.New("pasta: type mismatch")
	// ErrMultiplicity reports that a port cannot accept another link.
	ErrMultiplicity = errors.New("pasta: port multiplicity violation")
	// ErrCycle reports that a mutation would make the graph cyclic.
	ErrCycle = errors.New("pasta: graph cycle")
	// ErrInactive reports that an operation requires active objects.
	ErrInactive = errors.New("pasta: inactive object")
	// ErrInvalidMenu reports that a node menu document or update is malformed.
	ErrInvalidMenu = errors.New("pasta: invalid menu")
	// ErrStaleMenu reports that a menu update targeted an older menu version.
	ErrStaleMenu = errors.New("pasta: stale menu")
)

// Error describes a structured workspace failure.
type Error struct {
	// Op is the operation that failed.
	Op string
	// Phase is the validation, hook, or commit phase that failed.
	Phase string
	// Err is the underlying sentinel or wrapped error.
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Phase == "" {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + " " + e.Phase + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func opErr(op, phase string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Op: op, Phase: phase, Err: err}
}
