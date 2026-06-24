package webui

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const webUIImagineTicketTTL = time.Minute

var (
	webUIImagineNow     = time.Now
	webUIImagineTickets = newWebUIImagineTicketStore()
)

type webUIImagineTicketStore struct {
	mu      sync.Mutex
	tickets map[string]time.Time
}

func newWebUIImagineTicketStore() *webUIImagineTicketStore {
	return &webUIImagineTicketStore{tickets: map[string]time.Time{}}
}

func handleWebUIImagineWSTicket(w http.ResponseWriter, _ *http.Request) {
	ticket, err := webUIImagineTickets.Issue(webUIImagineNow())
	if err != nil {
		writeWebUIJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{"message": "Failed to create WebSocket ticket"},
		})
		return
	}
	writeWebUIJSON(w, http.StatusOK, map[string]any{
		"ticket":     ticket,
		"expires_in": int(webUIImagineTicketTTL.Seconds()),
	})
}

func (s *webUIImagineTicketStore) Issue(now time.Time) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	ticket := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.tickets[ticket] = now.Add(webUIImagineTicketTTL)
	return ticket, nil
}

func (s *webUIImagineTicketStore) Consume(ticket string, now time.Time) bool {
	if ticket == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt, ok := s.tickets[ticket]
	if ok {
		delete(s.tickets, ticket)
	}
	s.pruneLocked(now)
	return ok && now.Before(expiresAt)
}

func (s *webUIImagineTicketStore) pruneLocked(now time.Time) {
	for ticket, expiresAt := range s.tickets {
		if !now.Before(expiresAt) {
			delete(s.tickets, ticket)
		}
	}
}
