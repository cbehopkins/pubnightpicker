package cellar

import "context"

// Codec marshals and unmarshals strongly typed handler payloads.
type Codec[T any] interface {
	Marshal(value T) ([]byte, error)
	Unmarshal(raw []byte) (T, error)
}

// Handler executes business logic for a typed payload.
type Handler[T any] interface {
	Handle(ctx context.Context, payload T) Result
}
