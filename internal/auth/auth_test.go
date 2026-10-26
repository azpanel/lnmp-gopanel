package auth

import (
	"testing"
	"time"
)

func TestSessionExpiryAndLogout(t *testing.T) {
	s := New()
	now := time.Now()
	s.now = func() time.Time { return now }
	id, v := s.Create()
	if len(id) != 64 || len(v.CSRF) != 64 || id == v.CSRF {
		t.Fatal("session and CSRF tokens must be independent")
	}
	if _, ok := s.Get(id); !ok {
		t.Fatal("missing session")
	}
	s.Delete(id)
	if _, ok := s.Get(id); ok {
		t.Fatal("logout did not invalidate session")
	}
	id, _ = s.Create()
	now = now.Add(TTL)
	if _, ok := s.Get(id); ok {
		t.Fatal("expired session accepted")
	}
}
func TestSessionLimit(t *testing.T) {
	s := New()
	for i := 0; i < 200; i++ {
		s.Create()
	}
	if len(s.sessions) != 128 {
		t.Fatal("session storage should be bounded")
	}
}
