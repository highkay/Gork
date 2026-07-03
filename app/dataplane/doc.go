// Package dataplane contains runtime data-plane components for account selection,
// proxy selection, and upstream protocol execution. Dataplane code consumes
// control-plane snapshots and runtime directories; it does not directly own
// persistent account or proxy state.
package dataplane
