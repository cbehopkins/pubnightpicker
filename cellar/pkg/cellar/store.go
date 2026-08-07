package cellar

// Store persists cell metadata and execution outcomes.
type Store interface {
	SaveResult(cellID string, result Result) error
}
