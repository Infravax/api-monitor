// Package id generates identifiers for domain entities.
//
// It exists only to avoid duplicating the same UUID-generation logic across
// target, checker, and incident. It is a leaf utility with no business
// meaning of its own, which is why it lives on its own instead of inside a
// shared "models" package (this project deliberately avoids that pattern —
// see docs/architecture.md).
package id

import (
	"crypto/rand"
	"fmt"
)

// New returns a randomly generated RFC 4122 version 4 UUID, formatted as a
// lowercase hyphenated string. Generation uses only crypto/rand from the
// standard library, so no third-party UUID dependency is required.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read only fails if the OS entropy source is
		// unavailable, which is not a condition this service can
		// recover from.
		panic("id: failed to read random bytes: " + err.Error())
	}

	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
