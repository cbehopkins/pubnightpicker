package cellar

import "context"

// Registration executes and inspects a persisted cell using bound codec metadata.
// Implementations are expected to perform payload decoding before handler invocation.
type Registration interface {
	Execute(ctx context.Context, cell Cell) Result
	Inspect(cell Cell) Inspection
}

// Registry stores immutable handler bindings by HandlerName.
type Registry interface {
	Register(name HandlerName, registration Registration) error
	Lookup(name HandlerName) (Registration, bool)
	Freeze()
}
