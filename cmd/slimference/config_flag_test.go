package main

import (
	"reflect"
	"testing"
)

func TestExtractConfigFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantRest []string
	}{
		{
			name:     "no flag",
			args:     []string{"doctor"},
			wantPath: "",
			wantRest: []string{"doctor"},
		},
		{
			name:     "separated",
			args:     []string{"--config", "/tmp/x.toml", "doctor"},
			wantPath: "/tmp/x.toml",
			wantRest: []string{"doctor"},
		},
		{
			name:     "equals form",
			args:     []string{"--config=/tmp/y.toml", "version"},
			wantPath: "/tmp/y.toml",
			wantRest: []string{"version"},
		},
		{
			name:     "trailing with value missing keeps args",
			args:     []string{"--config"},
			wantPath: "",
			wantRest: []string{"--config"},
		},
		{
			name:     "flag after other flag",
			args:     []string{"--log-level", "debug", "--config", "/tmp/z.toml"},
			wantPath: "/tmp/z.toml",
			wantRest: []string{"--log-level", "debug"},
		},
		{
			name:     "other flags passed through",
			args:     []string{"--config", "/p", "--no-tui", "--port", "9090"},
			wantPath: "/p",
			wantRest: []string{"--no-tui", "--port", "9090"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotPath, gotRest := extractConfigFlag(tc.args)
			if gotPath != tc.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if !reflect.DeepEqual(gotRest, tc.wantRest) {
				t.Fatalf("rest = %v, want %v", gotRest, tc.wantRest)
			}
		})
	}
}
