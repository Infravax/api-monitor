// Package scheduler decides when each enabled target's next check is due
// and triggers it, based on that target's configured interval.
//
// It owns timing only: it does not perform HTTP requests itself (that's
// checker.Checker, consumed here through the small TargetChecker
// interface), and it does not interpret results — a CheckResult is simply
// handed to an optional callback, never inspected. What a result *means*
// (UP/DOWN, incidents, alerts) is decided elsewhere, in milestones not yet
// built.
package scheduler
