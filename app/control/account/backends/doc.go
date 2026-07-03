// Package backends contains adapter implementations and storage factory wiring
// for the account repository interface. External callers should depend on the
// account package seams while this package owns local SQLite, Redis, MySQL, and
// PostgreSQL details.
package backends
