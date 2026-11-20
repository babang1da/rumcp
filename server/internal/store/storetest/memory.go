// Package storetest provides an in-memory store.Repository for tests and dev.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
)

// Memory is an in-memory store.Repository.
type Memory struct {
	mu            sync.Mutex
	users         []store.User
	accounts      map[string]store.Account
	clients       map[string]store.OAuthClient
	tokens        map[string]store.OAuthToken
	consumed      map[string]bool
	calls         []call
	nextCallID    int64
	leads         []store.Lead
	usersByYandex map[string]string
	now           func() time.Time
}

type call struct {
	id        int64
	userID    string
	connector string
	tool      string
	status    string
	ts        time.Time
}

// New returns an empty in-memory store.
func New() *Memory {
	return &Memory{
		accounts:      map[string]store.Account{},
		clients:       map[string]store.OAuthClient{},
		tokens:        map[string]store.OAuthToken{},
		consumed:      map[string]bool{},
		usersByYandex: map[string]string{},
		now:           time.Now,
	}
}

var _ store.Repository = (*Memory)(nil)

// UpsertUser implements store.Repository.
func (m *Memory) UpsertUser(_ context.Context, u store.User) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id, ok := m.usersByYandex[u.YandexID]; ok {
		for i := range m.users {
			if m.users[i].ID == id {
				m.users[i].Login, m.users[i].Email, m.users[i].Name = u.Login, u.Email, u.Name
				return m.users[i], nil
			}
		}
	}
	u.ID = newID()
	u.CreatedAt = m.now()
	m.users = append(m.users, u)
	m.usersByYandex[u.YandexID] = u.ID
	return u, nil
}

// UserByID implements store.Repository.
func (m *Memory) UserByID(_ context.Context, id string) (store.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return store.User{}, store.ErrNotFound
}

// SaveAccount implements store.Repository.
func (m *Memory) SaveAccount(_ context.Context, a store.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.Provider == "" {
		a.Provider = "yandex"
	}
	a.ConnectedAt = m.now()
	m.accounts[a.UserID+"|"+a.Provider] = a
	return nil
}

// AccountByUser implements store.Repository.
func (m *Memory) AccountByUser(_ context.Context, userID, provider string) (store.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[userID+"|"+provider]
	if !ok {
		return store.Account{}, store.ErrNotFound
	}
	return a, nil
}

// DeleteAccount implements store.Repository.
func (m *Memory) DeleteAccount(_ context.Context, userID, provider string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.accounts, userID+"|"+provider)
	return nil
}

// CreateClient implements store.Repository.
func (m *Memory) CreateClient(_ context.Context, c store.OAuthClient) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.CreatedAt = m.now()
	m.clients[c.ClientID] = c
	return nil
}

// ClientByID implements store.Repository.
func (m *Memory) ClientByID(_ context.Context, clientID string) (store.OAuthClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[clientID]
	if !ok {
		return store.OAuthClient{}, store.ErrNotFound
	}
	return c, nil
}

// CreateToken implements store.Repository.
func (m *Memory) CreateToken(_ context.Context, t store.OAuthToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.ID = newID()
	t.CreatedAt = m.now()
	m.tokens[t.ID] = t
	return nil
}

// TokenByHash implements store.Repository.
func (m *Memory) TokenByHash(_ context.Context, hash, kind string) (store.OAuthToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.TokenHash == hash && t.Kind == kind {
			t.ConsumedAt = m.consumedAt(t)
			return t, nil
		}
	}
	return store.OAuthToken{}, store.ErrNotFound
}

// ConsumeToken implements store.Repository.
func (m *Memory) ConsumeToken(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consumed[id] {
		return store.ErrTokenConsumed
	}
	m.consumed[id] = true
	return nil
}

// RecordCall implements store.Repository.
func (m *Memory) RecordCall(_ context.Context, userID, connector, tool, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendCall(userID, connector, tool, status)
	return nil
}

// StartCall implements store.Repository.
func (m *Memory) StartCall(_ context.Context, userID, connector, tool, status string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendCall(userID, connector, tool, status), nil
}

// FinishCall implements store.Repository.
func (m *Memory) FinishCall(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.calls {
		if m.calls[i].id == id {
			m.calls[i].status = status
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *Memory) appendCall(userID, connector, tool, status string) int64 {
	m.nextCallID++
	m.calls = append(m.calls, call{
		id: m.nextCallID, userID: userID, connector: connector, tool: tool, status: status, ts: m.now(),
	})
	return m.nextCallID
}

// Usage implements store.Repository.
func (m *Memory) Usage(_ context.Context, userID string, since time.Time) (store.Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	usage := store.Usage{ByConnector: map[string]int{}}
	for _, c := range m.calls {
		if c.userID == userID && !c.ts.Before(since) {
			usage.CallsToday++
			usage.ByConnector[c.connector]++
		}
	}
	return usage, nil
}

// CreateLead implements store.Repository.
func (m *Memory) CreateLead(_ context.Context, l store.Lead) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l.ID = newID()
	l.CreatedAt = m.now()
	m.leads = append(m.leads, l)
	return nil
}

// Leads returns the stored leads, for assertions in tests.
func (m *Memory) Leads() []store.Lead {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.Lead(nil), m.leads...)
}

// Users returns the stored users, for assertions in tests.
func (m *Memory) Users() []store.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.User(nil), m.users...)
}

// AccountTokenEnc returns the sealed access token of a user, for assertions.
func (m *Memory) AccountTokenEnc(userID, provider string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[userID+"|"+provider]
	return a.AccessTokenEnc, ok
}

func (m *Memory) consumedAt(t store.OAuthToken) *time.Time {
	if m.consumed[t.ID] {
		ts := m.now()
		return &ts
	}
	return nil
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("storetest: id: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
