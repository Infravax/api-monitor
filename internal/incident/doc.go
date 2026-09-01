// Package incident owns the Incident domain type: a period during which a
// target is considered unhealthy. In a later milestone (M7) this package
// will also apply failure/recovery thresholds to a stream of check results
// to decide when to open or resolve an incident; for now it only defines
// the incident shape, its invariants, and the open/resolve transition.
package incident
