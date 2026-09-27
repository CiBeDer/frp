// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package managed stores server-managed client credentials and port grants.
package managed

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/naming"
	"github.com/fatedier/frp/pkg/util/util"
)

var (
	ErrInvalid       = errors.New("invalid managed client")
	ErrConflict      = errors.New("managed client conflict")
	ErrPersistence   = errors.New("managed client storage failure")
	ErrUnauthorized  = errors.New("managed client authorization failed")
	ErrDisabled      = errors.New("managed client disabled")
	ErrProxyDisabled = errors.New("managed proxy disabled")
	ErrNotFound      = errors.New("managed client not found")
)

type Proxy struct {
	Name       string `json:"name" toml:"name"`
	Type       string `json:"type" toml:"type"`
	LocalIP    string `json:"localIP" toml:"localIP"`
	LocalPort  int    `json:"localPort" toml:"localPort"`
	RemotePort int    `json:"remotePort" toml:"remotePort"`
	Enabled    *bool  `json:"enabled,omitempty" toml:"-"`
}

// Client contains a secret token. HTTP list responses must redact Token.
type Client struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Token      string  `json:"token,omitempty"`
	ServerAddr string  `json:"serverAddr"`
	Proxies    []Proxy `json:"proxies"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

type diskStore struct {
	Version int      `json:"version"`
	Clients []Client `json:"clients"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	allowedPorts []types.PortsRange
	clients      []Client
	events       []Event
	nextEventID  uint64
}

// NewStore opens the private credential file. An absent file starts an empty
// store; a corrupt or invalid file must never silently disable access control.
func NewStore(path string, allowedPorts []types.PortsRange) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: storage path is required", ErrInvalid)
	}
	s := &Store{path: path, allowedPorts: slices.Clone(allowedPorts), clients: []Client{}, events: []Event{}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: open: %w", ErrPersistence, err)
	}
	defer f.Close()

	var disk diskStore
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrPersistence, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: unexpected data after client store", ErrPersistence)
	}
	if disk.Version != 1 || disk.Clients == nil {
		return nil, fmt.Errorf("%w: unsupported or incomplete client store", ErrPersistence)
	}
	for _, client := range disk.Clients {
		if !validID(client.ID) || client.Token == "" {
			return nil, fmt.Errorf("%w: stored client has missing or invalid credentials", ErrPersistence)
		}
		if err := s.validate(&client); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPersistence, err)
		}
		if err := s.checkConflicts(client); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPersistence, err)
		}
		s.clients = append(s.clients, client)
	}
	return s, nil
}

func (s *Store) List() []Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make([]Client, len(s.clients))
	for i, client := range s.clients {
		clients[i] = cloneClient(client)
	}
	return clients
}

func (s *Store) Get(id string) (Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.ID == id {
			return cloneClient(client), true
		}
	}
	return Client{}, false
}

// Create commits the new credential and its port reservations together before
// making either visible to authentication or dashboard requests.
func (s *Store) Create(client Client) (Client, error) {
	client = cloneClient(client)
	if client.ID != "" {
		return Client{}, fmt.Errorf("%w: id is assigned by the server", ErrInvalid)
	}
	if err := s.validate(&client); err != nil {
		return Client{}, err
	}
	normalizeClientEnabled(&client, nil)
	var err error
	client.ID, err = randomHex(16)
	if err != nil {
		return Client{}, fmt.Errorf("%w: generate client id: %w", ErrPersistence, err)
	}
	if client.Token == "" {
		client.Token, err = randomHex(32)
		if err != nil {
			return Client{}, fmt.Errorf("%w: generate token: %w", ErrPersistence, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkConflicts(client); err != nil {
		return Client{}, err
	}
	clients := append(slices.Clone(s.clients), client)
	if err := s.save(clients); err != nil {
		return Client{}, err
	}
	s.clients = clients
	return cloneClient(client), nil
}

// Authenticate uses the same token digest as unmodified frpc. Claimed user,
// clientID and runID fields are deliberately not trusted for choosing grants.
func (s *Store) Authenticate(login *msg.Login) (Client, error) {
	if login == nil {
		return Client{}, ErrUnauthorized
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := -1
	for i, client := range s.clients {
		if util.ConstantTimeEqString(util.GetAuthKey(client.Token, login.Timestamp), login.PrivilegeKey) {
			index = i
		}
	}
	if index < 0 {
		return Client{}, ErrUnauthorized
	}
	client := cloneClient(s.clients[index])
	if !client.IsEnabled() {
		return client, ErrDisabled
	}
	return client, nil
}

// Authorize accepts only explicitly assigned fixed TCP/UDP ports. Dynamic
// allocation and load-balancing groups cannot bypass a client's reservations.
func (s *Store) Authorize(clientID string, m *msg.NewProxy) error {
	if m == nil || !validPort(m.RemotePort) || (m.ProxyType != "tcp" && m.ProxyType != "udp") ||
		m.Group != "" || m.GroupKey != "" {
		return fmt.Errorf("%w: only assigned fixed TCP/UDP ports without load balancing are permitted", ErrUnauthorized)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.ID != clientID {
			continue
		}
		if !client.IsEnabled() {
			return ErrDisabled
		}
		for _, proxy := range client.Proxies {
			expectedName := naming.AddUserPrefix(client.ID, proxy.Name)
			if proxy.Type != m.ProxyType || proxy.RemotePort != m.RemotePort || m.ProxyName != expectedName {
				continue
			}
			if !proxy.IsEnabled() {
				return ErrProxyDisabled
			}
			return nil
		}
		break
	}
	return fmt.Errorf("%w: requested port is not assigned to this client", ErrUnauthorized)
}

// TOML exports a configuration consumable by the official unmodified frpc.
// Explicit TOML tags are required: the native config structs use JSON tags.
func (s *Store) TOML(id string, serverPort int, scopes []v1.AuthScope) (string, error) {
	client, ok := s.Get(id)
	if !ok {
		return "", ErrNotFound
	}
	if !validPort(serverPort) {
		return "", fmt.Errorf("%w: invalid server port", ErrInvalid)
	}
	cfg := struct {
		ServerAddr string `toml:"serverAddr"`
		ServerPort int    `toml:"serverPort"`
		User       string `toml:"user"`
		Auth       struct {
			Method           string         `toml:"method"`
			Token            string         `toml:"token"`
			AdditionalScopes []v1.AuthScope `toml:"additionalScopes,omitempty"`
		} `toml:"auth"`
		Proxies []Proxy `toml:"proxies"`
	}{ServerAddr: client.ServerAddr, ServerPort: serverPort, User: client.ID, Proxies: enabledProxies(client.Proxies)}
	cfg.Auth.Method = "token"
	cfg.Auth.Token = client.Token
	cfg.Auth.AdditionalScopes = scopes
	b, err := toml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode client configuration: %w", err)
	}
	// frpc expands Go templates before parsing TOML. Keep literal '{{' in
	// credentials and proxy names from becoming a template expression. The
	// encoder emits single-line strings; convert literal strings to basic
	// strings before using TOML Unicode escapes for the delimiters.
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "{{") {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			quoted, _ := json.Marshal(value[1 : len(value)-1])
			value = string(quoted)
		}
		lines[i] = key + " = " + strings.ReplaceAll(value, "{{", `\u007b\u007b`)
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Store) validate(client *Client) error {
	client.Name = strings.TrimSpace(client.Name)
	client.ServerAddr = strings.TrimSpace(client.ServerAddr)
	if !validName(client.Name) {
		return fmt.Errorf("%w: name must contain 1 to 100 characters without control characters", ErrInvalid)
	}
	if !validHost(client.ServerAddr) {
		return fmt.Errorf("%w: serverAddr must be an IP address or hostname without scheme or port", ErrInvalid)
	}
	if len(client.Token) > 1024 || !utf8.ValidString(client.Token) {
		return fmt.Errorf("%w: token must be valid UTF-8 and at most 1024 bytes", ErrInvalid)
	}
	if len(client.Proxies) > 100 {
		return fmt.Errorf("%w: configure at most 100 proxies", ErrInvalid)
	}
	names := make(map[string]bool)
	ports := make(map[string]bool)
	for i := range client.Proxies {
		proxy := &client.Proxies[i]
		proxy.Name = strings.TrimSpace(proxy.Name)
		if !validName(proxy.Name) || names[proxy.Name] {
			return fmt.Errorf("%w: proxy names must be valid and unique within the client", ErrInvalid)
		}
		names[proxy.Name] = true
		if proxy.Type != "tcp" && proxy.Type != "udp" {
			return fmt.Errorf("%w: proxy type must be tcp or udp", ErrInvalid)
		}
		proxy.LocalIP = strings.TrimSpace(proxy.LocalIP)
		if proxy.LocalIP == "" {
			proxy.LocalIP = "127.0.0.1"
		}
		if !validHost(proxy.LocalIP) {
			return fmt.Errorf("%w: localIP must be an IP address or hostname without scheme or port", ErrInvalid)
		}
		if !validPort(proxy.LocalPort) || !validPort(proxy.RemotePort) {
			return fmt.Errorf("%w: localPort and remotePort must be between 1 and 65535", ErrInvalid)
		}
		if !s.portAllowed(proxy.RemotePort) {
			return fmt.Errorf("%w: remotePort %d is excluded by allowPorts", ErrInvalid, proxy.RemotePort)
		}
		key := portKey(*proxy)
		if ports[key] {
			return fmt.Errorf("%w: duplicate %s reservation", ErrConflict, key)
		}
		ports[key] = true
	}
	return nil
}

func (s *Store) checkConflicts(candidate Client) error {
	ports := make(map[string]bool, len(candidate.Proxies))
	for _, proxy := range candidate.Proxies {
		ports[portKey(proxy)] = true
	}
	for _, client := range s.clients {
		if client.ID == candidate.ID {
			return fmt.Errorf("%w: duplicate client id", ErrConflict)
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

func (s *Store) portAllowed(port int) bool {
	if len(s.allowedPorts) == 0 {
		return true
	}
	for _, allowed := range s.allowedPorts {
		if allowed.Single > 0 {
			if port == allowed.Single {
				return true
			}
		} else if port >= allowed.Start && port <= allowed.End {
			return true
		}
	}
	return false
}

func (s *Store) save(clients []Client) error {
	data, err := json.MarshalIndent(diskStore{Version: 1, Clients: clients}, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: encode: %w", ErrPersistence, err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%w: create directory: %w", ErrPersistence, err)
	}
	f, err := os.CreateTemp(dir, ".managed-clients-*")
	if err != nil {
		return fmt.Errorf("%w: create temporary file: %w", ErrPersistence, err)
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("%w: write: %w", ErrPersistence, err)
	}
	if closeErr != nil {
		return fmt.Errorf("%w: close: %w", ErrPersistence, closeErr)
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return fmt.Errorf("%w: replace: %w", ErrPersistence, err)
	}
	return nil
}

func cloneClient(client Client) Client {
	client.Enabled = cloneBool(client.Enabled)
	client.Proxies = slices.Clone(client.Proxies)
	for i := range client.Proxies {
		client.Proxies[i].Enabled = cloneBool(client.Proxies[i].Enabled)
	}
	return client
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func portKey(proxy Proxy) string {
	return fmt.Sprintf("%s/%d", proxy.Type, proxy.RemotePort)
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func validName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') &&
				(ch < 'A' || ch > 'Z') &&
				(ch < '0' || ch > '9') &&
				ch != '-' {
				return false
			}
		}
	}
	return true
}
