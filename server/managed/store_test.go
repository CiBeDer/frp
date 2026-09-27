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

package managed

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fatedier/frp/pkg/auth"
	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/naming"
)

func testClient(port int) Client {
	return Client{
		Name: "Office", ServerAddr: "frp.example.com",
		Proxies: []Proxy{{Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: port}},
	}
}

func newTestStore(t *testing.T, allowed []types.PortsRange) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "private", "clients.json"), allowed)
	require.NoError(t, err)
	return s
}

func TestCreatePersistsAndIsolatesSnapshots(t *testing.T) {
	s := newTestStore(t, nil)
	input := testClient(6000)
	client, err := s.Create(input)
	require.NoError(t, err)
	require.Len(t, client.Token, 64)
	require.True(t, validID(client.ID))
	require.Equal(t, "127.0.0.1", client.Proxies[0].LocalIP)

	input.Proxies[0].RemotePort = 9999
	client.Proxies[0].RemotePort = 9998
	listed := s.List()
	listed[0].Proxies[0].RemotePort = 9997
	got, ok := s.Get(client.ID)
	require.True(t, ok)
	require.Equal(t, 6000, got.Proxies[0].RemotePort)
	got.Proxies[0].RemotePort = 9996

	second, err := s.Create(testClient(6001))
	require.NoError(t, err)
	require.NotEqual(t, client.Token, second.Token)
	require.NotEqual(t, client.ID, second.ID)
	reloaded, err := NewStore(s.path, nil)
	require.NoError(t, err)
	require.Equal(t, s.List(), reloaded.List())
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestAuthenticateAndEnforcePortGrants(t *testing.T) {
	s := newTestStore(t, nil)
	first, err := s.Create(testClient(6000))
	require.NoError(t, err)
	second, err := s.Create(testClient(6001))
	require.NoError(t, err)
	login := &msg.Login{Timestamp: 1234567890, User: second.ID, ClientID: second.ID, RunID: "spoofed"}
	require.NoError(t, auth.NewTokenAuth(nil, first.Token).SetLogin(login))
	matched, err := s.Authenticate(login)
	require.NoError(t, err)
	require.Equal(t, first.ID, matched.ID, "grant selection must ignore claimed identity")
	matched.Proxies[0].RemotePort = 6001
	require.NoError(t, s.Authorize(first.ID, &msg.NewProxy{
		ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 6000,
	}))
	for name, proxy := range map[string]*msg.NewProxy{
		"another client's port": {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 6001},
		"unassigned port":       {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 7000},
		"wrong proxy name":      {ProxyName: naming.AddUserPrefix(first.ID, "other"), ProxyType: "tcp", RemotePort: 6000},
		"dynamic port":          {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 0},
		"negative port":         {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: -1},
		"overflow port":         {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 65536},
		"different protocol":    {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "udp", RemotePort: 6000},
		"http":                  {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "http", RemotePort: 6000},
		"stcp":                  {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "stcp", RemotePort: 6000},
		"group":                 {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 6000, Group: "shared"},
		"group key":             {ProxyName: naming.AddUserPrefix(first.ID, "ssh"), ProxyType: "tcp", RemotePort: 6000, GroupKey: "shared"},
		"nil":                   nil,
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, s.Authorize(first.ID, proxy), ErrUnauthorized)
		})
	}
	require.ErrorIs(t, s.Authorize("unknown", &msg.NewProxy{ProxyType: "tcp", RemotePort: 6000}), ErrUnauthorized)
	login.PrivilegeKey = strings.Repeat("0", 32)
	_, err = s.Authenticate(login)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = s.Authenticate(nil)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestRejectInvalidAndConflictingClients(t *testing.T) {
	for name, mutate := range map[string]func(*Client){
		"empty name":        func(c *Client) { c.Name = " " },
		"long name":         func(c *Client) { c.Name = strings.Repeat("a", 101) },
		"control name":      func(c *Client) { c.Name = "a\nb" },
		"provided id":       func(c *Client) { c.ID = "spoofed" },
		"server scheme":     func(c *Client) { c.ServerAddr = "https://frp.example.com" },
		"server port":       func(c *Client) { c.ServerAddr = "frp.example.com:7000" },
		"missing host":      func(c *Client) { c.ServerAddr = "" },
		"host path":         func(c *Client) { c.ServerAddr = "frp.example.com/path" },
		"proxy type":        func(c *Client) { c.Proxies[0].Type = "http" },
		"local port zero":   func(c *Client) { c.Proxies[0].LocalPort = 0 },
		"remote port zero":  func(c *Client) { c.Proxies[0].RemotePort = 0 },
		"remote port large": func(c *Client) { c.Proxies[0].RemotePort = 65536 },
		"local host scheme": func(c *Client) { c.Proxies[0].LocalIP = "http://localhost" },
		"duplicate name": func(c *Client) {
			c.Proxies = append(c.Proxies, Proxy{Name: "ssh", Type: "udp", LocalPort: 53, RemotePort: 6001})
		},
		"too many proxies": func(c *Client) { c.Proxies = make([]Proxy, 101) },
		"long token":       func(c *Client) { c.Token = strings.Repeat("x", 1025) },
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t, nil)
			client := testClient(6000)
			mutate(&client)
			_, err := s.Create(client)
			require.ErrorIs(t, err, ErrInvalid)
			require.Empty(t, s.List())
		})
	}
	s := newTestStore(t, nil)
	client, err := s.Create(testClient(6000))
	require.NoError(t, err)
	_, err = s.Create(testClient(6000))
	require.ErrorIs(t, err, ErrConflict)
	duplicateToken := testClient(6001)
	duplicateToken.Token = client.Token
	_, err = s.Create(duplicateToken)
	require.ErrorIs(t, err, ErrConflict)
	duplicatePort := testClient(6002)
	duplicatePort.Proxies = append(duplicatePort.Proxies, Proxy{Name: "second", Type: "tcp", LocalPort: 80, RemotePort: 6002})
	_, err = s.Create(duplicatePort)
	require.ErrorIs(t, err, ErrConflict)
	udp := testClient(6000)
	udp.Proxies[0].Type = "udp"
	_, err = s.Create(udp)
	require.NoError(t, err, "TCP and UDP have separate port namespaces")
}

func TestAllowPortsAndFailureDoesNotCommit(t *testing.T) {
	s := newTestStore(t, []types.PortsRange{{Start: 6000, End: 6002}, {Single: 7000}})
	for _, port := range []int{6000, 6002, 7000} {
		_, err := s.Create(testClient(port))
		require.NoError(t, err)
	}
	_, err := s.Create(testClient(6500))
	require.ErrorIs(t, err, ErrInvalid)
	before := s.List()
	s.path = t.TempDir() // Replacing a directory with the credential file must fail.
	_, err = s.Create(testClient(6001))
	require.ErrorIs(t, err, ErrPersistence)
	require.Equal(t, before, s.List())
	entries, err := os.ReadDir(filepath.Dir(s.path))
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), ".managed-clients-", "failed commits clean up temporary credentials")
	}
}

func TestCorruptStoreFailsClosed(t *testing.T) {
	s := newTestStore(t, nil)
	client, err := s.Create(testClient(6000))
	require.NoError(t, err)
	duplicate, err := json.Marshal(diskStore{Version: 1, Clients: []Client{client, client}})
	require.NoError(t, err)
	missingToken := cloneClient(client)
	missingToken.Token = ""
	missing, err := json.Marshal(diskStore{Version: 1, Clients: []Client{missingToken}})
	require.NoError(t, err)
	invalidStores := []string{
		"",
		"{",
		"null",
		`{"version":2,"clients":[]}`,
		`{"version":1,"clients":null}`,
		`{"version":1,"clients":[]} {}`,
		`{"version":1,"clients":[],"extra":true}`,
		string(duplicate),
		string(missing),
	}
	for _, contents := range invalidStores {
		require.NoError(t, os.WriteFile(s.path, []byte(contents), 0o600))
		_, err := NewStore(s.path, nil)
		require.ErrorIs(t, err, ErrPersistence)
	}
	valid, err := json.Marshal(diskStore{Version: 1, Clients: []Client{client}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.path, valid, 0o600))
	_, err = NewStore(s.path, []types.PortsRange{{Single: 7000}})
	require.ErrorIs(t, err, ErrPersistence, "narrowing allowPorts cannot silently retain invalid grants")
}

func TestExportLoadsWithOfficialClientParser(t *testing.T) {
	s := newTestStore(t, nil)
	input := testClient(6000)
	input.Token = "token\"with\\escapes\nand unicode 密钥{{ .Envs.SECRET }}"
	input.ServerAddr = "2001:db8::1"
	input.Proxies[0].Name = "ssh\"\\服务{{literal}}"
	input.Proxies = append(input.Proxies, Proxy{Name: "dns", Type: "udp", LocalIP: "localhost", LocalPort: 53, RemotePort: 6000})
	client, err := s.Create(input)
	require.NoError(t, err)
	scopes := []v1.AuthScope{v1.AuthScopeHeartBeats, v1.AuthScopeNewWorkConns}
	output, err := s.TOML(client.ID, 7000, scopes)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "frpc.toml")
	require.NoError(t, os.WriteFile(path, []byte(output), 0o600))
	loaded, proxies, _, legacy, err := config.LoadClientConfig(path, true)
	require.NoError(t, err)
	require.False(t, legacy)
	require.Equal(t, client.ID, loaded.User)
	// Identity is assigned by frps from the token; omit the newer clientID option
	// so the generated configuration also works with older official frpc releases.
	require.Empty(t, loaded.ClientID)
	require.Equal(t, input.ServerAddr, loaded.ServerAddr)
	require.Equal(t, 7000, loaded.ServerPort)
	require.Equal(t, input.Token, loaded.Auth.Token)
	require.Equal(t, scopes, loaded.Auth.AdditionalScopes)
	require.Len(t, proxies, 2)
	for i, proxy := range proxies {
		require.NoError(t, validation.ValidateProxyConfigurerForClient(proxy))
		require.Equal(t, client.Proxies[i].Name, proxy.GetBaseConfig().Name)
		require.Equal(t, client.Proxies[i].LocalPort, proxy.GetBaseConfig().LocalPort)
		m := &msg.NewProxy{}
		proxy.MarshalToMsg(m)
		m.ProxyName = naming.AddUserPrefix(client.ID, m.ProxyName)
		require.NoError(t, s.Authorize(client.ID, m))
	}
	login := &msg.Login{Timestamp: 1234567890}
	require.NoError(t, auth.NewTokenAuth(loaded.Auth.AdditionalScopes, loaded.Auth.Token).SetLogin(login))
	matched, err := s.Authenticate(login)
	require.NoError(t, err)
	require.Equal(t, client.ID, matched.ID)
}

func TestConcurrentReservationsHaveOneWinner(t *testing.T) {
	s := newTestStore(t, nil)
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := s.Create(testClient(6000))
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.True(t, errors.Is(err, ErrConflict))
		}
	}
	require.Equal(t, 1, successes)
	require.Len(t, s.List(), 1)
	reloaded, err := NewStore(s.path, nil)
	require.NoError(t, err)
	require.Equal(t, s.List(), reloaded.List())
}

func TestManagedLifecycleMutationsAndBackupRestore(t *testing.T) {
	s := newTestStore(t, nil)
	client, err := s.Create(Client{
		Name:       "Office",
		ServerAddr: "frp.example.com",
		Proxies: []Proxy{
			{Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: 6000},
			{Name: "dns", Type: "udp", LocalPort: 53, RemotePort: 6001},
		},
	})
	require.NoError(t, err)
	require.True(t, client.IsEnabled())
	require.True(t, client.Proxies[0].IsEnabled())

	authorizedSSH := &msg.NewProxy{
		ProxyName:  naming.AddUserPrefix(client.ID, "ssh"),
		ProxyType:  "tcp",
		RemotePort: 6000,
	}
	require.NoError(t, s.Authorize(client.ID, authorizedSSH))

	client, err = s.SetProxyEnabled(client.ID, "ssh", false)
	require.NoError(t, err)
	require.False(t, client.Proxies[0].IsEnabled())
	require.ErrorIs(t, s.Authorize(client.ID, authorizedSSH), ErrProxyDisabled)
	toml, err := s.TOML(client.ID, 7000, nil)
	require.NoError(t, err)
	require.NotContains(t, toml, "name = 'ssh'")
	require.Contains(t, toml, "name = 'dns'")

	login := &msg.Login{Timestamp: 1234567890}
	require.NoError(t, auth.NewTokenAuth(nil, client.Token).SetLogin(login))
	client, err = s.SetClientEnabled(client.ID, false)
	require.NoError(t, err)
	_, err = s.Authenticate(login)
	require.ErrorIs(t, err, ErrDisabled)

	client, err = s.SetClientEnabled(client.ID, true)
	require.NoError(t, err)
	updated, err := s.Update(client.ID, Client{
		Name:       "Office renamed",
		ServerAddr: "new.example.com",
		Proxies: []Proxy{
			{
				Name:       "ssh-new",
				Type:       "tcp",
				LocalIP:    "10.0.0.5",
				LocalPort:  2222,
				RemotePort: 6010,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, client.Token, updated.Token)
	require.Equal(t, "Office renamed", updated.Name)
	require.Equal(t, 6010, updated.Proxies[0].RemotePort)

	oldToken := updated.Token
	rotated, err := s.RotateToken(updated.ID)
	require.NoError(t, err)
	require.NotEqual(t, oldToken, rotated.Token)

	oldLogin := &msg.Login{Timestamp: 1234567890}
	require.NoError(t, auth.NewTokenAuth(nil, oldToken).SetLogin(oldLogin))
	_, err = s.Authenticate(oldLogin)
	require.ErrorIs(t, err, ErrUnauthorized)

	newLogin := &msg.Login{Timestamp: 1234567890}
	require.NoError(t, auth.NewTokenAuth(nil, rotated.Token).SetLogin(newLogin))
	_, err = s.Authenticate(newLogin)
	require.NoError(t, err)

	backup := s.Backup()
	require.Len(t, backup.Clients, 1)
	deleted, err := s.Delete(rotated.ID)
	require.NoError(t, err)
	require.Equal(t, rotated.ID, deleted.ID)
	require.Empty(t, s.List())

	restored, err := s.Restore(backup)
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(t, rotated.ID, restored[0].ID)
	require.Equal(t, rotated.Token, restored[0].Token)

	s.RecordEvent(Event{
		Type:     "test_event",
		ClientID: rotated.ID,
		Message:  "hello",
	})
	events := s.Events(rotated.ID, 10)
	require.Len(t, events, 1)
	require.Equal(t, "test_event", events[0].Type)
	require.Equal(t, "Office renamed", events[0].ClientName)
}

func TestLegacyStoreDefaultsEnabledAndAllowsNoProxies(t *testing.T) {
	s := newTestStore(t, nil)
	client, err := s.Create(Client{
		Name:       "Empty",
		ServerAddr: "frp.example.com",
	})
	require.NoError(t, err)
	require.True(t, client.IsEnabled())
	require.Empty(t, client.Proxies)

	legacy := diskStore{
		Version: 1,
		Clients: []Client{
			{
				ID:         client.ID,
				Name:       "Legacy",
				Token:      client.Token,
				ServerAddr: "frp.example.com",
				Proxies: []Proxy{
					{
						Name:       "ssh",
						Type:       "tcp",
						LocalPort:  22,
						RemotePort: 6000,
					},
				},
			},
		},
	}
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.path, data, 0o600))

	reloaded, err := NewStore(s.path, nil)
	require.NoError(t, err)
	got, ok := reloaded.Get(client.ID)
	require.True(t, ok)
	require.True(t, got.IsEnabled())
	require.True(t, got.Proxies[0].IsEnabled())
}
