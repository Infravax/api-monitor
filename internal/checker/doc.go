// Package checker owns the CheckResult domain type (the outcome of one
// monitoring attempt against a target) and, as of Milestone 3, the Checker
// type that actually performs an HTTP/HTTPS request and produces one.
//
// Checker answers "what happened when we attempted this request" — it does
// not decide whether a target is UP or DOWN, does not persist results,
// does not schedule itself, and does not send alerts. Those are the future
// responsibilities of the scheduler (M4), storage, and the incident/alert
// managers (M7/M8) respectively; this package stays a pure observation
// producer so those layers can be added without changing it.
package checker
