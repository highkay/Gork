// Package products contains shared product-layer glue for account selection,
// dispatch, retry policy, and console stream transport.
//
// Public OpenAI- or Anthropic-compatible wire formats stay in their product
// packages; reverse protocol parsing and network transports stay in dataplane.
package products
