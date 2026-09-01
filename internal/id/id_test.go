package id

import "testing"

func TestNew(t *testing.T) {
	a := New()
	b := New()

	if a == "" {
		t.Fatal("New() returned an empty string")
	}
	if len(a) != 36 {
		t.Fatalf("New() = %q, want a 36-character UUID string", a)
	}
	if a == b {
		t.Fatalf("New() returned the same value twice: %q", a)
	}
}
