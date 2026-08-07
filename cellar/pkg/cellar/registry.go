package cellar

// Registry stores and resolves durable cells.
type Registry interface {
	Register(cell Cell) error
	Lookup(id string) (Cell, bool)
}
