package target

import (
	"errors"
	"testing"
	"time"
)

func validParams() NewParams {
	return NewParams{
		Name:               "Example API",
		URL:                "https://api.example.com/health",
		Method:             "GET",
		Interval:           30 * time.Second,
		Timeout:            5 * time.Second,
		ExpectedStatusCode: 200,
	}
}

func TestNew_Valid(t *testing.T) {
	tg, err := New(validParams())
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if tg.ID == "" {
		t.Error("New() did not assign an ID")
	}
	if !tg.Enabled {
		t.Error("New() should default Enabled to true")
	}
}

func TestNew_Defaults(t *testing.T) {
	p := validParams()
	p.Method = ""
	p.ExpectedStatusCode = 0

	tg, err := New(p)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if tg.Method != "GET" {
		t.Errorf("Method = %q, want default GET", tg.Method)
	}
	if tg.ExpectedStatusCode != 200 {
		t.Errorf("ExpectedStatusCode = %d, want default 200", tg.ExpectedStatusCode)
	}
}

func TestNew_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(p *NewParams)
		wantErr error
	}{
		{
			name:    "empty name",
			mutate:  func(p *NewParams) { p.Name = "" },
			wantErr: ErrEmptyName,
		},
		{
			name:    "empty url",
			mutate:  func(p *NewParams) { p.URL = "" },
			wantErr: ErrEmptyURL,
		},
		{
			name:    "malformed url",
			mutate:  func(p *NewParams) { p.URL = "api.example.com" },
			wantErr: ErrInvalidURL,
		},
		{
			name:    "unsupported protocol",
			mutate:  func(p *NewParams) { p.URL = "ftp://api.example.com" },
			wantErr: ErrUnsupportedScheme,
		},
		{
			name:    "explicit unsupported method",
			mutate:  func(p *NewParams) { p.Method = "TRACE" },
			wantErr: ErrUnsupportedMethod,
		},
		{
			name:    "zero interval",
			mutate:  func(p *NewParams) { p.Interval = 0 },
			wantErr: ErrInvalidInterval,
		},
		{
			name:    "negative interval",
			mutate:  func(p *NewParams) { p.Interval = -time.Second },
			wantErr: ErrInvalidInterval,
		},
		{
			name:    "zero timeout",
			mutate:  func(p *NewParams) { p.Timeout = 0 },
			wantErr: ErrInvalidTimeout,
		},
		{
			name:    "invalid expected status code",
			mutate:  func(p *NewParams) { p.ExpectedStatusCode = 999 },
			wantErr: ErrInvalidExpectedStatusCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validParams()
			tt.mutate(&p)

			_, err := New(p)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_DirectConstruction covers invariants that New's defaulting
// logic prevents from being reached through New itself (e.g. an empty
// method, which New defaults to GET).
func TestValidate_DirectConstruction(t *testing.T) {
	tests := []struct {
		name    string
		target  Target
		wantErr error
	}{
		{
			name: "empty id",
			target: Target{
				Name: "x", URL: "https://a.com", Method: "GET",
				Interval: time.Second, Timeout: time.Second, ExpectedStatusCode: 200,
			},
			wantErr: ErrEmptyID,
		},
		{
			name: "empty method",
			target: Target{
				ID: "id", Name: "x", URL: "https://a.com", Method: "",
				Interval: time.Second, Timeout: time.Second, ExpectedStatusCode: 200,
			},
			wantErr: ErrEmptyMethod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.target.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
