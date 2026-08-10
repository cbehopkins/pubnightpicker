package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cellar/internal/sqlite"
	"cellar/pkg/cellar"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "inspect":
		if err := runInspect(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "inspect error:", err)
			os.Exit(1)
		}
	default:
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func runInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var inputPath string
	var sqlitePath string
	var id string
	var jsonHandlers string

	fs.StringVar(&inputPath, "input", "", "path to JSON snapshot of active cells")
	fs.StringVar(&sqlitePath, "sqlite", "", "path to SQLite database")
	fs.StringVar(&id, "id", "", "optional cell ID to inspect")
	fs.StringVar(&jsonHandlers, "json-handlers", "", "comma-separated handler names to decode as JSON")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if inputPath == "" && sqlitePath == "" {
		return errors.New("one of -input or -sqlite is required")
	}
	if inputPath != "" && sqlitePath != "" {
		return errors.New("-input and -sqlite are mutually exclusive")
	}

	store, err := buildInspectStore(inputPath, sqlitePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	inspector := cellar.NewCellInspector()
	for _, name := range parseCSV(jsonHandlers) {
		inspector.RegisterDecoder(cellar.HandlerName(name), cellar.JSONAnyDecoder())
	}

	if id != "" {
		cell, err := store.Get(cellar.CellID(id))
		if err != nil {
			return err
		}
		view := inspectionView(inspector.InspectCell(cell))
		return writeJSON(os.Stdout, view)
	}

	inspections, err := inspector.InspectAll(store)
	if err != nil {
		return err
	}

	views := make([]inspectionOutput, 0, len(inspections))
	for _, inspection := range inspections {
		views = append(views, inspectionView(inspection))
	}
	sort.Slice(views, func(i, j int) bool {
		return views[i].ID < views[j].ID
	})

	return writeJSON(os.Stdout, views)
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  cellar inspect (-input <cells.json> | -sqlite <cells.db>) [-id <cell-id>] [-json-handlers handlerA,handlerB]")
}

func buildInspectStore(inputPath, sqlitePath string) (cellar.DebuggableStore, error) {
	if sqlitePath != "" {
		db, err := sql.Open("sqlite", sqlitePath)
		if err != nil {
			return nil, err
		}

		store, err := sqlite.NewStore(db, nil)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		return store, nil
	}

	cells, err := loadCellsSnapshot(inputPath)
	if err != nil {
		return nil, err
	}

	store := cellar.NewMemoryStore(nil)
	for _, cell := range cells {
		if err := store.ForceUpdate(cell); err != nil {
			return nil, fmt.Errorf("load snapshot cell %s: %w", cell.ID, err)
		}
	}
	return store, nil
}

func loadCellsSnapshot(path string) ([]cellar.Cell, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var payload []snapshotCell
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	cells := make([]cellar.Cell, 0, len(payload))
	for i, entry := range payload {
		cell, err := entry.toCell()
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

type snapshotCell struct {
	ID          string  `json:"id"`
	HandlerName string  `json:"handlerName"`
	Payload     []byte  `json:"payload"`
	State       string  `json:"state"`
	NotBefore   *string `json:"notBefore"`
}

func (s snapshotCell) toCell() (cellar.Cell, error) {
	if s.ID == "" {
		return cellar.Cell{}, errors.New("id is required")
	}
	if s.HandlerName == "" {
		return cellar.Cell{}, errors.New("handlerName is required")
	}

	var notBefore *time.Time
	if s.NotBefore != nil && strings.TrimSpace(*s.NotBefore) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *s.NotBefore)
		if err != nil {
			return cellar.Cell{}, fmt.Errorf("invalid notBefore: %w", err)
		}
		notBefore = &parsed
	}

	return cellar.Cell{
		ID:          cellar.CellID(s.ID),
		HandlerName: cellar.HandlerName(s.HandlerName),
		Payload:     s.Payload,
		State:       cellar.CellState(s.State),
		NotBefore:   notBefore,
	}, nil
}

type inspectionOutput struct {
	ID            cellar.CellID      `json:"id"`
	HandlerName   cellar.HandlerName `json:"handlerName"`
	State         cellar.CellState   `json:"state"`
	NotBefore     *time.Time         `json:"notBefore,omitempty"`
	PayloadFormat string             `json:"payloadFormat"`
	Payload       any                `json:"payload"`
	DecodeError   string             `json:"decodeError,omitempty"`
}

func inspectionView(inspection cellar.Inspection) inspectionOutput {
	var decodeError string
	if inspection.DecodeError != nil {
		decodeError = inspection.DecodeError.Error()
	}

	return inspectionOutput{
		ID:            inspection.Cell.ID,
		HandlerName:   inspection.Cell.HandlerName,
		State:         inspection.Cell.State,
		NotBefore:     inspection.Cell.NotBefore,
		PayloadFormat: inspection.PayloadFormat,
		Payload:       inspection.Payload,
		DecodeError:   decodeError,
	}
}

func writeJSON(out *os.File, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
