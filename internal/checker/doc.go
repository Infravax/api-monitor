// Package checker will perform the actual HTTP/HTTPS request against a
// target and measure the outcome: latency, response status, connection
// errors, and timeouts.
//
// It has no knowledge of scheduling, storage, or incidents — its only job
// is "given a target, run one check and report the raw result."
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package checker
