package cellar

import "errors"

// MemoryRegistry is an in-memory handler registry.
type MemoryRegistry struct {
	registrations map[HandlerName]Registration
	frozen        bool
}

// NewMemoryRegistry creates a mutable in-memory registry.
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{registrations: make(map[HandlerName]Registration)}
}

// Register stores a registration by handler name.
func (r *MemoryRegistry) Register(name HandlerName, registration Registration) error {
	if r.frozen {
		return ErrRegistryFrozen
	}
	if registration == nil {
		return ErrRegistrationNil
	}
	r.registrations[name] = registration
	return nil
}

// Lookup returns the registration for the requested handler name.
func (r *MemoryRegistry) Lookup(name HandlerName) (Registration, bool) {
	registration, ok := r.registrations[name]
	return registration, ok
}

// Freeze prevents further registration changes.
func (r *MemoryRegistry) Freeze() {
	r.frozen = true
}

var (
	ErrRegistryFrozen  = errors.New("registry is frozen")
	ErrRegistrationNil = errors.New("registration is nil")
)
