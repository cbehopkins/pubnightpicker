package cellar

// Handler executes a cell and returns its result.
type Handler interface {
	Handle(ctx Context, cell Cell) (Result, error)
}
