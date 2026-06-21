package compression

import (
	"strings"
	"testing"
)

// TestCompactJSONContent exercises the compactJSONContent function across a range of inputs.
func TestCompactJSONContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		wantSavedGT   int    // savings must be strictly greater than this value
		wantUnchanged bool   // true if the function should return the original text
		wantContains  string // non-empty: result must contain this substring
	}{
		{
			name: "valid JSON with whitespace",
			input: `{
  "key": "value",
  "number": 42
}`,
			wantSavedGT:   0,
			wantUnchanged: false,
		},
		{
			name:          "non-JSON text",
			input:         "This is plain text, not JSON at all.",
			wantSavedGT:   -1,
			wantUnchanged: true,
		},
		{
			name:          "empty string",
			input:         "",
			wantSavedGT:   -1,
			wantUnchanged: true,
		},
		{
			name:          "already compact JSON - savings below threshold",
			input:         `{"a":1,"b":2}`,
			wantSavedGT:   -1,
			wantUnchanged: true, // no savings -> returned unchanged
		},
		{
			name: "large JSON with nested objects",
			input: `{
  "level1": {
    "level2": {
      "level3": {
        "value": "some deeply nested content here",
        "list": [
          "item one",
          "item two",
          "item three",
          "item four",
          "item five"
        ]
      }
    }
  },
  "another_key": "another_value",
  "third_key":   "third_value"
}`,
			wantSavedGT: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, saved := compactJSONContent(tc.input)

			if tc.wantUnchanged {
				if got != tc.input {
					t.Errorf("expected original text returned, got %q", truncate(got, 80))
				}
				if saved != 0 {
					t.Errorf("expected saved=0, got %d", saved)
				}
				return
			}

			if saved <= tc.wantSavedGT {
				t.Errorf("saved = %d, want > %d", saved, tc.wantSavedGT)
			}
			if tc.wantContains != "" && !strings.Contains(got, tc.wantContains) {
				t.Errorf("result does not contain %q", tc.wantContains)
			}
			// Compacted JSON must not contain unescaped newlines or leading spaces.
			if strings.Contains(got, "\n") {
				t.Errorf("compacted JSON contains newline: %q", truncate(got, 80))
			}
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
