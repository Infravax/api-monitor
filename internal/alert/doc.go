
// Package alert will notify external systems (starting with webhooks) when
// an incident opens or resolves.
//
// It reacts to state transitions produced by the incident package and is
// responsible for delivery concerns such as deduplication and retries. It
// does not decide UP/DOWN state itself.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package alert
