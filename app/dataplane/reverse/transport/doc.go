// Package transport contains reverse dataplane network adapters.
//
// This layer owns HTTP, grpc-web, websocket, media, asset, and LiveKit
// transport mechanics. Protocol DTO conversion stays in reverse/protocol,
// endpoint defaults stay in reverse/runtime, and account/proxy scheduling stays
// in control or dataplane proxy packages.
package transport
