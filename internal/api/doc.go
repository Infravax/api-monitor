// Package api exposes the service's REST API. As of Milestone 2 it covers
// target management (create/read/update/delete) and a liveness health
// check; querying check results and incidents will follow in later
// milestones.
//
// It is a thin transport layer — request parsing, response encoding, and
// mapping domain errors to HTTP status codes — that delegates all real
// work to target.Service. It contains no business logic of its own.
package api
