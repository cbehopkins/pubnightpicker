package runtime

import (
	"context"

	"cellar/pkg/cellar"
)

type typedRegistration[T any] struct {
	codec   cellar.Codec[T]
	handler cellar.Handler[T]
}

func (r typedRegistration[T]) Execute(ctx context.Context, cell cellar.Cell) cellar.Result {
	if r.codec == nil {
		return cellar.ErrorResult{Message: "codec is nil"}
	}
	if r.handler == nil {
		return cellar.ErrorResult{Message: "handler is nil"}
	}
	payload, err := r.codec.Unmarshal(cell.Payload)
	if err != nil {
		return cellar.ErrorResult{Message: "decode payload", Err: err}
	}
	return r.handler.Handle(ctx, payload)
}

func (r typedRegistration[T]) Inspect(cell cellar.Cell) cellar.Inspection {
	if r.codec == nil {
		return cellar.Inspection{Cell: cell, PayloadFormat: "unknown"}
	}
	payload, err := r.codec.Unmarshal(cell.Payload)
	if err != nil {
		return cellar.Inspection{Cell: cell, PayloadFormat: "json", DecodeError: err}
	}
	return cellar.Inspection{Cell: cell, PayloadFormat: "json", Payload: payload}
}

func RegisterJSON[T any](registry cellar.Registry, name cellar.HandlerName, handler cellar.Handler[T]) error {
	return registry.Register(name, typedRegistration[T]{
		codec:   cellar.JSONCodec[T](),
		handler: handler,
	})
}
