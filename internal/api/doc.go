// Package api will expose the service's REST API: managing targets and
// querying check results and incidents.
//
// It is a thin transport layer — request/response handling and validation —
// that delegates all real work to the target, incident, and storage
// packages.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package api
