// Package target will own monitoring target management: creating, reading,
// updating, deleting, and validating the APIs that the monitor is
// configured to check (URL, method, expected status, check interval, etc.).
//
// It is the source of truth for "what should be checked." No other package
// should define or mutate target configuration.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package target
