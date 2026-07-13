// Package anthropic adapts Anthropic-compatible product endpoints to shared
// account selection and reverse data-plane components.
//
// Router code decodes Anthropic request shapes, while the messages layer
// converts them into the OpenAI chat stream contract and formats Anthropic
// responses on the way back out.
package anthropic
