package monitor

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

func valid() Input {
	return Input{
		Name:            "Example API",
		URL:             "https://example.com/health",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		Enabled:         boolPtr(true),
	}
}

func boolPtr(b bool) *bool { return &b }

func TestNormalize(t *testing.T) {
	tests := []struct {
		name        string
		in          Input
		wantName    string
		wantURL     string
		wantTimeout int
		wantEnabled bool
	}{
		{
			name: "trims spaces and applies defaults",
			in: Input{
				Name:            "  Example API \t",
				URL:             "\n https://example.com/health  ",
				IntervalSeconds: 60,
			},
			wantName:    "Example API",
			wantURL:     "https://example.com/health",
			wantTimeout: 10,
			wantEnabled: true,
		},
		{
			name: "keeps explicit timeout and enabled=false",
			in: Input{
				Name:            "Example API",
				URL:             "https://example.com/health",
				IntervalSeconds: 60,
				TimeoutSeconds:  5,
				Enabled:         boolPtr(false),
			},
			wantName:    "Example API",
			wantURL:     "https://example.com/health",
			wantTimeout: 5,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			in.Normalize()

			if in.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", in.Name, tt.wantName)
			}
			if in.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", in.URL, tt.wantURL)
			}
			if in.TimeoutSeconds != tt.wantTimeout {
				t.Errorf("TimeoutSeconds = %d, want %d", in.TimeoutSeconds, tt.wantTimeout)
			}
			if in.Enabled == nil {
				t.Fatalf("Enabled = nil, want pointer to %v", tt.wantEnabled)
			}
			if *in.Enabled != tt.wantEnabled {
				t.Errorf("*Enabled = %v, want %v", *in.Enabled, tt.wantEnabled)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		modify     func(in *Input)
		wantFields []string
	}{
		// --- должно проходить ---
		{
			name:   "valid https",
			modify: func(in *Input) {},
		},
		{
			name:   "http localhost with port, path and query",
			modify: func(in *Input) { in.URL = "http://localhost:8080/ping?x=1" },
		},
		{
			name:   "interval lower bound 30",
			modify: func(in *Input) { in.IntervalSeconds = 30 },
		},
		{
			name:   "interval upper bound 86400",
			modify: func(in *Input) { in.IntervalSeconds = 86400 },
		},
		{
			name:   "name of 100 cyrillic runes",
			modify: func(in *Input) { in.Name = strings.Repeat("я", 100) },
		},

		// --- name ---
		{
			name:       "name of 101 cyrillic runes",
			modify:     func(in *Input) { in.Name = strings.Repeat("я", 101) },
			wantFields: []string{"name"},
		},
		{
			name:       "empty name",
			modify:     func(in *Input) { in.Name = "" },
			wantFields: []string{"name"},
		},

		// --- url ---
		{
			name:       "url without scheme",
			modify:     func(in *Input) { in.URL = "example.com" },
			wantFields: []string{"url"},
		},
		{
			name:       "url with ftp scheme",
			modify:     func(in *Input) { in.URL = "ftp://example.com" },
			wantFields: []string{"url"},
		},
		{
			name:       "url without host",
			modify:     func(in *Input) { in.URL = "http://" },
			wantFields: []string{"url"},
		},
		{
			name:       "unparsable url",
			modify:     func(in *Input) { in.URL = "::not a url::" },
			wantFields: []string{"url"},
		},

		// --- interval_seconds ---
		{
			name:       "interval below min (29)",
			modify:     func(in *Input) { in.IntervalSeconds = 29 },
			wantFields: []string{"interval_seconds"},
		},
		{
			name:       "interval above max (86401)",
			modify:     func(in *Input) { in.IntervalSeconds = 86401 },
			wantFields: []string{"interval_seconds"},
		},

		// --- timeout_seconds ---
		{
			name:       "timeout zero without Normalize",
			modify:     func(in *Input) { in.TimeoutSeconds = 0 },
			wantFields: []string{"timeout_seconds"},
		},
		{
			name:       "timeout above max (61)",
			modify:     func(in *Input) { in.TimeoutSeconds = 61 },
			wantFields: []string{"timeout_seconds"},
		},
		{
			name: "timeout not less than interval (30/30)",
			modify: func(in *Input) {
				in.IntervalSeconds = 30
				in.TimeoutSeconds = 30
			},
			wantFields: []string{"timeout_seconds"},
		},

		// --- несколько ошибок сразу ---
		{
			name: "empty name and bad url",
			modify: func(in *Input) {
				in.Name = ""
				in.URL = "example.com"
			},
			wantFields: []string{"name", "url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			tt.modify(&in)

			err := in.Validate()

			if tt.wantFields == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() = nil, want errors in fields %v", tt.wantFields)
			}

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error type = %T (%v), want *ValidationError", err, err)
			}

			got := make([]string, 0, len(ve.Fields))
			for k := range ve.Fields {
				got = append(got, k)
			}
			sort.Strings(got)

			want := append([]string(nil), tt.wantFields...)
			sort.Strings(want)

			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("error fields = %v, want exactly %v (details: %v)", got, want, ve.Fields)
			}
		})
	}
}
