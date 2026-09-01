package target

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/InfraVex/api-monitor/internal/id"
)

// Target represents an HTTP/HTTPS endpoint that API Monitor checks.
type Target struct {
	ID                 string
	Name               string
	URL                string
	Method             string
	Interval           time.Duration
	Timeout            time.Duration
	ExpectedStatusCode int
	Enabled            bool
}

// Sentinel errors for Target validation failures, so callers (and tests)
// can check the specific rule that was violated with errors.Is instead of
// matching on error strings.
var (
	ErrEmptyID                   = errors.New("target: id is required")
	ErrEmptyName                 = errors.New("target: name is required")
	ErrEmptyURL                  = errors.New("target: url is required")
	ErrInvalidURL                = errors.New("target: url must be an absolute url with a host")
	ErrUnsupportedScheme         = errors.New("target: url scheme must be http or https")
	ErrEmptyMethod               = errors.New("target: method is required")
	ErrUnsupportedMethod         = errors.New("target: unsupported http method")
	ErrInvalidInterval           = errors.New("target: interval must be greater than zero")
	ErrInvalidTimeout            = errors.New("target: timeout must be greater than zero")
	ErrInvalidExpectedStatusCode = errors.New("target: expected status code must be between 100 and 599")
)

// allowedMethods are the HTTP methods that are meaningful for a health
// check. CONNECT and TRACE are intentionally excluded.
var allowedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// NewParams holds the inputs for New. A struct is used instead of
// positional parameters because Interval and Timeout share a type
// (time.Duration) and are easy to swap by accident at a call site; named
// fields remove that risk.
type NewParams struct {
	Name               string
	URL                string
	Method             string
	Interval           time.Duration
	Timeout            time.Duration
	ExpectedStatusCode int
}

// New creates a Target, applying defaults for Method (GET) and
// ExpectedStatusCode (200) when left unset, and validates the result before
// returning it. Interval and Timeout have no default: they are policy
// decisions the caller must make explicitly.
func New(p NewParams) (Target, error) {
	method := p.Method
	if method == "" {
		method = http.MethodGet
	}

	expectedStatusCode := p.ExpectedStatusCode
	if expectedStatusCode == 0 {
		expectedStatusCode = http.StatusOK
	}

	t := Target{
		ID:                 id.New(),
		Name:               strings.TrimSpace(p.Name),
		URL:                strings.TrimSpace(p.URL),
		Method:             strings.ToUpper(strings.TrimSpace(method)),
		Interval:           p.Interval,
		Timeout:            p.Timeout,
		ExpectedStatusCode: expectedStatusCode,
		Enabled:            true,
	}

	if err := t.Validate(); err != nil {
		return Target{}, err
	}
	return t, nil
}

// Validate checks that the Target satisfies all domain invariants. It is
// exported separately from New so a Target reconstructed from elsewhere
// (e.g. loaded from storage in a later milestone) can be re-checked without
// going through construction defaults again.
func (t Target) Validate() error {
	if t.ID == "" {
		return ErrEmptyID
	}
	if t.Name == "" {
		return ErrEmptyName
	}
	if t.URL == "" {
		return ErrEmptyURL
	}
	if err := validateURL(t.URL); err != nil {
		return err
	}
	if t.Method == "" {
		return ErrEmptyMethod
	}
	if !allowedMethods[t.Method] {
		return ErrUnsupportedMethod
	}
	if t.Interval <= 0 {
		return ErrInvalidInterval
	}
	if t.Timeout <= 0 {
		return ErrInvalidTimeout
	}
	if t.ExpectedStatusCode < 100 || t.ExpectedStatusCode > 599 {
		return ErrInvalidExpectedStatusCode
	}
	return nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrUnsupportedScheme
	}
	return nil
}
