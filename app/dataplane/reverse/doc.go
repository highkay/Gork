// Package reverse contains planning, classification, feedback helpers, and
// executor glue for the Grok reverse data plane. The executor owns account
// lease and proxy lease lifetimes; protocol and transport packages do not own
// persistent state.
package reverse
