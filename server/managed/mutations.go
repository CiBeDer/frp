package managed

import (
	"fmt"
	"slices"
	"time"
)

const maxRecentEvents = 200

type Event struct {
	ID         uint64 `json:"id"`
	Time       string `json:"time"`
	Type       string `json:"type"`
	Level      string `json:"level"`
	ClientID   string `json:"clientID,omitempty"`
	ClientName string `json:"clientName,omitempty"`
	ProxyName  string `json:"proxyName,omitempty"`
	Message    string `json:"message"`
}

type Backup struct {
	Version int      `json:"version"`
	Clients []Client `json:"clients"`
}

func boolPtr(v bool) *bool {
	out := v
	return &out
}

func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	return boolPtr(*v)
}

func (c Client) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func (p Proxy) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}

func normalizeClientEnabled(client *Client, existing *Client) {
	if client.Enabled == nil {
		if existing != nil {
			client.Enabled = cloneBool(existing.Enabled)
		} else {
			client.Enabled = boolPtr(true)
		}
	}
	existingProxies := map[string]Proxy{}
	if existing != nil {
		for _, proxy := range existing.Proxies {
			existingProxies[proxy.Name] = proxy
		}
	}
	for i := range client.Proxies {
		if client.Proxies[i].Enabled != nil {
			continue
		}
		if old, ok := existingProxies[client.Proxies[i].Name]; ok {
			client.Proxies[i].Enabled = cloneBool(old.Enabled)
		} else {
			client.Proxies[i].Enabled = boolPtr(true)
		}
	}
}

func enabledProxies(proxies []Proxy) []Proxy {
	out := make([]Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if !proxy.IsEnabled() {
			continue
		}
		proxy.Enabled = nil
		out = append(out, proxy)
	}
	return out
}

func (s *Store) findClientIndexLocked(id string) int {
	for i := range s.clients {
		if s.clients[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) checkConflictsExcept(candidate Client, ignoreID string) error {
	ports := make(map[string]bool, len(candidate.Proxies))
	for _, proxy := range candidate.Proxies {
		ports[portKey(proxy)] = true
	}
	for _, client := range s.clients {
		if client.ID == ignoreID {
			continue
		}
		if client.Token == candidate.Token {
			return fmt.Errorf("%w: token is already assigned to another client", ErrConflict)
		}
		for _, proxy := range client.Proxies {
			if ports[portKey(proxy)] {
				return fmt.Errorf("%w: %s is already assigned to another client", ErrConflict, portKey(proxy))
			}
		}
	}
	return nil
}

func (s *Store) Update(id string, replacement Client) (Client, error) {
	replacement = cloneClient(replacement)
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findClientIndexLocked(id)
	if index < 0 {
		return Client{}, ErrNotFound
	}
	existing := s.clients[index]
	replacement.ID = existing.ID
	replacement.Token = existing.Token
	normalizeClientEnabled(&replacement, &existing)
	if err := s.validate(&replacement); err != nil {
		return Client{}, err
	}
	if err := s.checkConflictsExcept(replacement, id); err != nil {
		return Client{}, err
	}
	clients := slices.Clone(s.clients)
	clients[index] = replacement
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return cloneClient(replacement), nil
}

func (s *Store) SetClientEnabled(id string, enabled bool) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findClientIndexLocked(id)
	if index < 0 {
		return Client{}, ErrNotFound
	}
	clients := slices.Clone(s.clients)
	client := cloneClient(clients[index])
	client.Enabled = boolPtr(enabled)
	clients[index] = client
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return cloneClient(client), nil
}

func (s *Store) SetProxyEnabled(id, proxyName string, enabled bool) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findClientIndexLocked(id)
	if index < 0 {
		return Client{}, ErrNotFound
	}
	clients := slices.Clone(s.clients)
	client := cloneClient(clients[index])
	found := false
	for i := range client.Proxies {
		if client.Proxies[i].Name == proxyName {
			client.Proxies[i].Enabled = boolPtr(enabled)
			found = true
			break
		}
	}
	if !found {
		return Client{}, ErrNotFound
	}
	clients[index] = client
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return cloneClient(client), nil
}

func (s *Store) Delete(id string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findClientIndexLocked(id)
	if index < 0 {
		return Client{}, ErrNotFound
	}
	deleted := cloneClient(s.clients[index])
	clients := make([]Client, 0, len(s.clients)-1)
	clients = append(clients, s.clients[:index]...)
	clients = append(clients, s.clients[index+1:]...)
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return deleted, nil
}

func (s *Store) RotateToken(id string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.findClientIndexLocked(id)
	if index < 0 {
		return Client{}, ErrNotFound
	}
	client := cloneClient(s.clients[index])
	for {
		token, err := randomHex(32)
		if err != nil {
			return Client{}, fmt.Errorf("%w: generate token: %w", ErrPersistence, err)
		}
		if s.tokenAssignedExcept(token, id) {
			continue
		}
		client.Token = token
		break
	}
	clients := slices.Clone(s.clients)
	clients[index] = client
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return cloneClient(client), nil
}

func (s *Store) tokenAssignedExcept(token, ignoreID string) bool {
	for _, client := range s.clients {
		if client.ID != ignoreID && client.Token == token {
			return true
		}
	}
	return false
}

func (s *Store) Backup() Backup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make([]Client, len(s.clients))
	for i := range s.clients {
		clients[i] = cloneClient(s.clients[i])
	}
	return Backup{Version: 1, Clients: clients}
}

func (s *Store) Restore(backup Backup) ([]Client, error) {
	if backup.Version != 1 || backup.Clients == nil {
		return nil, fmt.Errorf("%w: unsupported or incomplete backup", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := &Store{
		path:         s.path,
		allowedPorts: slices.Clone(s.allowedPorts),
		clients:      []Client{},
	}
	for _, raw := range backup.Clients {
		client := cloneClient(raw)
		if !validID(client.ID) || client.Token == "" {
			return nil, fmt.Errorf("%w: backup contains invalid credentials", ErrInvalid)
		}
		normalizeClientEnabled(&client, nil)
		if err := candidate.validate(&client); err != nil {
			return nil, err
		}
		if err := candidate.checkConflicts(client); err != nil {
			return nil, err
		}
		candidate.clients = append(candidate.clients, client)
	}
	if err := s.save(candidate.clients); err != nil {
		return nil, err
	}
	s.clients = candidate.clients

	out := make([]Client, len(s.clients))
	for i := range s.clients {
		out[i] = cloneClient(s.clients[i])
	}
	return out, nil
}

func (s *Store) RecordEvent(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEventID++
	event.ID = s.nextEventID
	if event.Time == "" {
		event.Time = time.Now().UTC().Format(time.RFC3339)
	}
	if event.Level == "" {
		event.Level = "info"
	}
	if event.ClientName == "" && event.ClientID != "" {
		if index := s.findClientIndexLocked(event.ClientID); index >= 0 {
			event.ClientName = s.clients[index].Name
		}
	}
	s.events = append(s.events, event)
	if len(s.events) > maxRecentEvents {
		s.events = slices.Clone(s.events[len(s.events)-maxRecentEvents:])
	}
}

func (s *Store) Events(clientID string, limit int) []Event {
	if limit <= 0 || limit > maxRecentEvents {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, 0, limit)
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		event := s.events[i]
		if clientID != "" && event.ClientID != clientID {
			continue
		}
		out = append(out, event)
	}
	return out
}
