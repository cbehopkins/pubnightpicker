package cellar

// Result is a marker interface for values produced by cells.
type Result interface {
	Name() string
}
