// Package scheduler marks the runtime scheduler boundary.
//
// The scheduler does not own durable state. Runtime authority stays in the
// trigger, supervision, recovery, jobs, approvals, and SQLite services; the
// operator-facing scheduler tick composes those existing services.
package scheduler
