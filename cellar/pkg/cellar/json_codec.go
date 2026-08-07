package cellar

import "encoding/json"

// JSONCodec returns a JSON codec for payload type T.
func JSONCodec[T any]() Codec[T] {
	return jsonCodec[T]{}
}

type jsonCodec[T any] struct{}

func (jsonCodec[T]) Marshal(value T) ([]byte, error) {
	return json.Marshal(value)
}

func (jsonCodec[T]) Unmarshal(raw []byte) (T, error) {
	var value T
	err := json.Unmarshal(raw, &value)
	return value, err
}

// JSONAnyDecoder decodes payload bytes into map/list/scalar JSON structures for inspection.
func JSONAnyDecoder() PayloadDecoder {
	return PayloadDecoderFunc{
		Name: "json",
		Fn: func(raw []byte) (any, error) {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return value, nil
		},
	}
}
