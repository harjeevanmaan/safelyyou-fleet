// Package devices loads the static fleet definition from a flat text file
// (one device ID per line, with a header row) and exposes a read-only
// lookup by device ID. The registry is built once at startup and is safe
// for concurrent reads thereafter.
package devices

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// Registry is an immutable set of known device IDs.
type Registry struct {
	ids map[string]struct{}
}

// LoadCSV reads devices.csv: a header line followed by one device ID per line.
func LoadCSV(path string) (*Registry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open devices csv: %w", err)
	}
	defer file.Close()
	return parseCSV(file)
}

func parseCSV(reader io.Reader) (*Registry, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Scan() // skip header

	ids := make(map[string]struct{})
	for scanner.Scan() {
		ids[scanner.Text()] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read devices csv: %w", err)
	}
	return &Registry{ids: ids}, nil
}

// Has reports whether the given device ID is part of the fleet.
func (r *Registry) Has(deviceID string) bool {
	_, found := r.ids[deviceID]
	return found
}

// IDs returns a snapshot of the known device IDs (unordered).
func (r *Registry) IDs() []string {
	result := make([]string, 0, len(r.ids))
	for deviceID := range r.ids {
		result = append(result, deviceID)
	}
	return result
}

// Len returns the number of devices in the registry.
func (r *Registry) Len() int { return len(r.ids) }
