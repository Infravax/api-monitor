// Package target owns monitoring target management: the Target domain
// type, its validation rules, and (in a later milestone) creating, reading,
// updating, and deleting them.
//
// It is the source of truth for "what should be checked." No other package
// should define or mutate target configuration.
package target
