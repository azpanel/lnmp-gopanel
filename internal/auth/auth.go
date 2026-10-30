package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

const CookieName = "lnmp_session"
const TTL = 8 * time.Hour

type Session struct {
	CSRF    string
	Expires time.Time
}
type Store struct {
	mu       sync.Mutex
	sessions map[string]Session
	now      func() time.Time
}

func New() *Store { return &Store{sessions: map[string]Session{}, now: time.Now} }
func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (s *Store) Create() (string, Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.sessions {
		if !s.now().Before(v.Expires) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 128 {
		var oldest string
		var t time.Time
		for k, v := range s.sessions {
			if oldest == "" || v.Expires.Before(t) {
				oldest = k
				t = v.Expires
			}
		}
		delete(s.sessions, oldest)
	}
	id := token()
	v := Session{CSRF: token(), Expires: s.now().Add(TTL)}
	s.sessions[id] = v
	return id, v
}
func (s *Store) Get(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if ok && !s.now().Before(v.Expires) {
		delete(s.sessions, id)
		return Session{}, false
	}
	return v, ok
}
func (s *Store) Delete(id string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.sessions, id) }
func (s *Store) DeleteAll()       { s.mu.Lock(); defer s.mu.Unlock(); clear(s.sessions) }
func Equal(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Limiter bounds authenticated account-security operations independently of login.
type Limiter struct {
	mu       sync.Mutex
	attempts []time.Time
}

const MaxLoginSources = 4096

// SourceLimiter has a separate rolling window for each direct client address.
// Its zero value is ready to use. Live windows are never evicted to admit new
// sources, which would let an attacker reset a window by filling the table.
type SourceLimiter struct {
	mu        sync.Mutex
	sources   map[string][]time.Time
	now       func() time.Time
	nextPrune time.Time
}

func (l *SourceLimiter) Allow(source string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	cut := now.Add(-time.Minute)
	if l.sources == nil {
		l.sources = make(map[string][]time.Time)
	}
	if !now.Before(l.nextPrune) {
		for key, attempts := range l.sources {
			if !attempts[len(attempts)-1].After(cut) {
				delete(l.sources, key)
			}
		}
		l.nextPrune = now.Add(time.Second)
	}
	attempts, exists := l.sources[source]
	if !exists && len(l.sources) >= MaxLoginSources {
		return false, time.Minute
	}
	i := 0
	for i < len(attempts) && !attempts[i].After(cut) {
		i++
	}
	attempts = attempts[i:]
	if len(attempts) >= 10 {
		return false, attempts[0].Add(time.Minute).Sub(now)
	}
	l.sources[source] = append(attempts, now)
	return true, 0
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cut := now.Add(-time.Minute)
	i := 0
	for i < len(l.attempts) && l.attempts[i].Before(cut) {
		i++
	}
	l.attempts = l.attempts[i:]
	if len(l.attempts) >= 10 {
		return false
	}
	l.attempts = append(l.attempts, now)
	return true
}
