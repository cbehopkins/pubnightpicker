// Package cellar defines the public API for durable Cell execution primitives.
package cellar

// Context carries execution-scoped values for cell runtimes.
type Context interface{}

// Cell represents a durable executable unit managed by the runtime.
type Cell interface {
	ID() string
	Run(ctx Context) (Result, error)
}
