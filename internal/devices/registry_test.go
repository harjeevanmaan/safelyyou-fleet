package devices

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParseCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "happy path",
			input: "device_id\nabc\ndef\nghi\n",
			want:  []string{"abc", "def", "ghi"},
		},
		{
			name:  "deduplicates repeated IDs",
			input: "device_id\nabc\nabc\nabc\n",
			want:  []string{"abc"},
		},
		{
			name:  "empty file yields zero devices",
			input: "",
			want:  []string{},
		},
		{
			name:  "header only yields zero devices",
			input: "device_id\n",
			want:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg, err := parseCSV(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := reg.IDs()
			sort.Strings(got)
			sort.Strings(tc.want)
			if !equalStrings(got, tc.want) {
				t.Fatalf("ids = %v, want %v", got, tc.want)
			}
			for _, id := range tc.want {
				if !reg.Has(id) {
					t.Fatalf("Has(%q) = false, want true", id)
				}
			}
		})
	}
}

func TestLoadCSVFromFile(t *testing.T) {
	t.Parallel()
	// Reads the real fixture so a malformed checked-in file fails CI here
	// rather than at simulator runtime.
	path := filepath.Join("..", "..", "data", "devices.csv")
	reg, err := LoadCSV(path)
	if err != nil {
		t.Fatalf("LoadCSV(%q): %v", path, err)
	}
	if reg.Len() == 0 {
		t.Fatalf("expected at least one device, got zero")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
