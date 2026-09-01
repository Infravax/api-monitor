// Package checker owns the CheckResult domain type: the outcome of one
// monitoring attempt against a target. In a later milestone this package
// will also perform the actual HTTP/HTTPS request that produces a
// CheckResult; for now it only defines the result shape and its
// invariants.
package checker
