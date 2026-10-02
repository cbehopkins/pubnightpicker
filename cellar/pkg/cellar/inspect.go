package cellar

import (
	"encoding/base64"
	"errors"
	"unicode/utf8"
)

// Inspection describes what a debugger can currently infer about a persisted cell payload.
type Inspection struct {
	Cell          Cell
	Payload       any
	PayloadFormat string
	DecodeError   error
}

// PayloadDecoder decodes persisted payload bytes for debug inspection.
type PayloadDecoder interface {
	Decode(raw []byte) (any, error)
	FormatName() string
}

// PayloadDecoderFunc adapts a function into a PayloadDecoder.
type PayloadDecoderFunc struct {
	Name string
	Fn   func(raw []byte) (any, error)
}

func (f PayloadDecoderFunc) Decode(raw []byte) (any, error) {
	if f.Fn == nil {
		return nil, errors.New("decoder function is nil")
	}
	return f.Fn(raw)
}

func (f PayloadDecoderFunc) FormatName() string {
	if f.Name == "" {
		return "custom"
	}
	return f.Name
}

// CellInspector decodes payloads using handler-specific decoders when available.
type CellInspector struct {
	decoders map[HandlerName]PayloadDecoder
}

// NewCellInspector creates a read-only inspector.
func NewCellInspector() *CellInspector {
	return &CellInspector{
		decoders: make(map[HandlerName]PayloadDecoder),
	}
}

// RegisterDecoder registers a decoder for a specific handler name.
func (i *CellInspector) RegisterDecoder(name HandlerName, decoder PayloadDecoder) {
	i.decoders[name] = decoder
}

// InspectCell inspects one persisted cell.
func (i *CellInspector) InspectCell(cell Cell) Inspection {
	step := currentStep(cell)
	if decoder, ok := i.decoders[step.HandlerName]; ok {
		payload, err := decoder.Decode(step.Payload)
		return Inspection{
			Cell:          cloneCell(cell),
			Payload:       payload,
			PayloadFormat: decoder.FormatName(),
			DecodeError:   err,
		}
	}

	if utf8.Valid(step.Payload) {
		return Inspection{
			Cell:          cloneCell(cell),
			Payload:       string(step.Payload),
			PayloadFormat: "utf8",
		}
	}

	return Inspection{
		Cell:          cloneCell(cell),
		Payload:       base64.StdEncoding.EncodeToString(step.Payload),
		PayloadFormat: "base64",
	}
}

func currentStep(cell Cell) CellStep {
	if cell.CurrentStep >= 0 && cell.CurrentStep < len(cell.Steps) {
		return cell.Steps[cell.CurrentStep]
	}
	return CellStep{}
}

// InspectAll inspects all active cells from the provided store.
func (i *CellInspector) InspectAll(store DebuggableStore) ([]Inspection, error) {
	cells, err := store.ListAll()
	if err != nil {
		return nil, err
	}

	inspections := make([]Inspection, 0, len(cells))
	for _, cell := range cells {
		inspections = append(inspections, i.InspectCell(cell))
	}
	return inspections, nil
}
