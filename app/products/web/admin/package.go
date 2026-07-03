// Package admin implements the HTTP Admin API runtime binding surface.
//
// It adapts HTTP requests to control-plane repositories, runtime services, and
// dataplane transports; those lower layers own persistence, account state, and
// network protocol details.
package admin
