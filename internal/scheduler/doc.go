// Package scheduler will decide when each target's next check is due and
// trigger the checker accordingly, based on each target's configured
// interval.
//
// It owns timing only. It does not perform HTTP requests itself and does
// not interpret results — that belongs to checker and the result
// processing/incident logic.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package scheduler
