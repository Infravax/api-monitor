// Package worker provides a bounded pool of goroutines that execute
// checks submitted to it, so the number of API checks running
// concurrently is capped regardless of how many targets are due at once.
//
// It sits between the Scheduler (which decides *when* a target is due)
// and the Checker (which knows *how* to perform one check): as of
// Milestone 5, the Scheduler submits work here instead of executing
// checks itself, and this package bounds how many of those submissions
// run at the same time.
package worker
