package cellar

// Scheduler coordinates when cells are selected for execution.
type Scheduler interface {
	Schedule(ctx Context, cell Cell) error
}
