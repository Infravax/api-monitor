// Package incident will translate a stream of check results into UP/DOWN
// state for a target, applying failure/recovery thresholds, and will own
// the lifecycle of an incident (opened, ongoing, resolved).
//
// It consumes results produced by the checker/result-processing path and
// emits state transitions for the alert manager to act on.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package incident
