package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/fatedier/frp/pkg/config/types"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/naming"
	httppkg "github.com/fatedier/frp/pkg/util/http"
	"github.com/fatedier/frp/server/managed"
	serverproxy "github.com/fatedier/frp/server/proxy"
	"github.com/fatedier/frp/server/registry"
)

type managedProxyManagerStub struct {
	online map[string]bool
}

func (m *managedProxyManagerStub) GetByName(name string) (serverproxy.Proxy, bool) {
	return nil, m.online[name]
}

func TestManagedClientAPI(t *testing.T) {
	c := NewController(&v1.ServerConfig{BindPort: 7000}, registry.NewClientRegistry(), nil)
	request := func(handler httppkg.APIHandler, body, mediaType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v2/managed-clients", strings.NewReader(body))
		r.Header.Set("Content-Type", mediaType)
		w := httptest.NewRecorder()
		httppkg.MakeHTTPHandlerFuncV2(handler)(w, r)
		return w
	}
	w := request(c.APIV2ManagedClientList, "", "")
	require.Contains(t, w.Body.String(), `"enabled":false`)
	require.Equal(t, http.StatusConflict, request(c.APIV2ManagedClientCreate, `{}`, "application/json").Code)
	store, err := managed.NewStore(filepath.Join(t.TempDir(), "clients.json"), nil)
	require.NoError(t, err)
	c.SetManagedClients(store)
	body := `{
		"name":"Office",
		"token":"never-list-this-secret",
		"serverAddr":"127.0.0.1",
		"proxies":[
			{"name":"ssh","type":"tcp","localIP":"127.0.0.1","localPort":22,"remotePort":16001},
			{"name":"dns","type":"udp","localIP":"127.0.0.1","localPort":53,"remotePort":16001}
		]
	}`
	require.Equal(t, http.StatusUnsupportedMediaType, request(c.APIV2ManagedClientCreate, body, "text/plain").Code)
	require.Equal(t, http.StatusBadRequest, request(c.APIV2ManagedClientCreate, body+`{}`, "application/json").Code)
	w = request(c.APIV2ManagedClientCreate, body, "application/json")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "never-list-this-secret")
	var created v2EnvelopeForTest[managedClientResponse]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Data.ID)
	require.True(t, created.Data.Enabled)
	require.Len(t, created.Data.Proxies, 2)
	require.False(t, created.Data.Proxies[0].Online)
	require.Equal(t, http.StatusConflict, request(c.APIV2ManagedClientCreate, body, "application/json").Code)
	require.Equal(t, http.StatusConflict, request(c.APIV2ManagedClientCreate,
		strings.ReplaceAll(body, "16001", "7000"), "application/json").Code)

	c.clientRegistry.Register(created.Data.ID, created.Data.ID, "run", "pc", "test", "127.0.0.1", "v1")
	c.pxyManager = &managedProxyManagerStub{
		online: map[string]bool{
			naming.AddUserPrefix(created.Data.ID, "ssh"): true,
		},
	}
	w = request(c.APIV2ManagedClientList, "", "")
	require.Contains(t, w.Body.String(), `"online":true`)
	require.NotContains(t, w.Body.String(), "never-list-this-secret")
	var listed v2EnvelopeForTest[struct {
		Items []managedClientResponse `json:"items"`
	}]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Data.Items, 1)
	require.True(t, listed.Data.Items[0].Proxies[0].Online)
	require.False(t, listed.Data.Items[0].Proxies[1].Online)
	r := httptest.NewRequest(http.MethodGet, "/config", nil)
	r = mux.SetURLVars(r, map[string]string{"id": created.Data.ID})
	w = httptest.NewRecorder()
	httppkg.MakeHTTPHandlerFuncV2(c.APIV2ManagedClientConfig)(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Contains(t, w.Body.String(), "never-list-this-secret")
	r = mux.SetURLVars(r, map[string]string{"id": "missing"})
	w = httptest.NewRecorder()
	httppkg.MakeHTTPHandlerFuncV2(c.APIV2ManagedClientConfig)(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}

type managedRuntimeRecorder struct {
	disconnected  []string
	disconnectAll int
}

func (r *managedRuntimeRecorder) DisconnectManagedClient(clientID string) {
	r.disconnected = append(r.disconnected, clientID)
}

func (r *managedRuntimeRecorder) DisconnectAllManagedClients() {
	r.disconnectAll++
}

func TestManagedProxyGrantChanged(t *testing.T) {
	enabled := true
	disabled := false
	base := []managed.Proxy{{
		Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1",
		LocalPort: 22, RemotePort: 16001, Enabled: &enabled,
	}}
	require.False(t, managedProxyGrantChanged(base, []managed.Proxy{{
		Name: "ssh", Type: "tcp", LocalIP: "10.0.0.2",
		LocalPort: 2222, RemotePort: 16001, Enabled: &enabled,
	}}), "local endpoint changes are client-side only")
	require.True(t, managedProxyGrantChanged(base, nil))
	require.True(t, managedProxyGrantChanged(base, []managed.Proxy{{
		Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1",
		LocalPort: 22, RemotePort: 16002, Enabled: &enabled,
	}}))
	require.True(t, managedProxyGrantChanged(base, []managed.Proxy{{
		Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1",
		LocalPort: 22, RemotePort: 16001, Enabled: &disabled,
	}}))
}

func TestManagedLifecycleAndPortPoolAPI(t *testing.T) {
	cfg := &v1.ServerConfig{
		BindPort:   7000,
		AllowPorts: []types.PortsRange{{Start: 16000, End: 16003}},
	}
	c := NewController(cfg, registry.NewClientRegistry(), nil)
	store, err := managed.NewStore(filepath.Join(t.TempDir(), "clients.json"), cfg.AllowPorts)
	require.NoError(t, err)
	client, err := store.Create(managed.Client{
		Name: "Office", ServerAddr: "frp.example.com",
		Proxies: []managed.Proxy{{
			Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: 16000,
		}},
	})
	require.NoError(t, err)
	c.SetManagedClients(store)
	c.pxyManager = &managedProxyManagerStub{
		online: map[string]bool{
			naming.AddUserPrefix(client.ID, "ssh"): true,
		},
	}
	runtime := &managedRuntimeRecorder{}
	c.SetManagedRuntime(runtime)

	request := func(
		method, path string,
		handler httppkg.APIHandler,
		body string,
		vars map[string]string,
	) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if vars != nil {
			req = mux.SetURLVars(req, vars)
		}
		w := httptest.NewRecorder()
		httppkg.MakeHTTPHandlerFuncV2(handler)(w, req)
		return w
	}

	w := request(http.MethodGet, "/api/v2/managed-ports", c.APIV2ManagedPortPool, "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var pool v2EnvelopeForTest[struct {
		SuggestedTCP int `json:"suggestedTCP"`
		Allocated    []struct {
			Port   int  `json:"port"`
			Online bool `json:"online"`
		} `json:"allocated"`
	}]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pool))
	require.Equal(t, 16001, pool.Data.SuggestedTCP)
	require.Len(t, pool.Data.Allocated, 1)
	require.Equal(t, 16000, pool.Data.Allocated[0].Port)
	require.True(t, pool.Data.Allocated[0].Online)

	updateBody := `{
		"name":"Office",
		"serverAddr":"frp.example.com",
		"proxies":[
			{
				"name":"ssh",
				"type":"tcp",
				"localIP":"127.0.0.1",
				"localPort":22,
				"remotePort":16002,
				"enabled":true
			}
		]
	}`
	w = request(
		http.MethodPut,
		"/api/v2/managed-clients/"+client.ID,
		c.APIV2ManagedClientUpdate,
		updateBody,
		map[string]string{"id": client.ID},
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{client.ID}, runtime.disconnected)

	w = request(
		http.MethodPatch,
		"/api/v2/managed-clients/"+client.ID+"/proxy-enabled",
		c.APIV2ManagedProxyEnabled,
		`{"name":"ssh","enabled":false}`,
		map[string]string{"id": client.ID},
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []string{client.ID, client.ID}, runtime.disconnected)
}

func TestManagedDeleteReleasesPort(t *testing.T) {
	cfg := &v1.ServerConfig{
		AllowPorts: []types.PortsRange{{Start: 16000, End: 16001}},
	}
	c := NewController(cfg, registry.NewClientRegistry(), nil)
	store, err := managed.NewStore(filepath.Join(t.TempDir(), "clients.json"), cfg.AllowPorts)
	require.NoError(t, err)
	client, err := store.Create(managed.Client{
		Name:       "Office",
		ServerAddr: "frp.example.com",
		Proxies: []managed.Proxy{{
			Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: 16000,
		}},
	})
	require.NoError(t, err)
	c.SetManagedClients(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v2/managed-clients/"+client.ID, nil)
	req = mux.SetURLVars(req, map[string]string{"id": client.ID})
	w := httptest.NewRecorder()
	httppkg.MakeHTTPHandlerFuncV2(c.APIV2ManagedClientDelete)(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	poolReq := httptest.NewRequest(http.MethodGet, "/api/v2/managed-ports", nil)
	poolWriter := httptest.NewRecorder()
	httppkg.MakeHTTPHandlerFuncV2(c.APIV2ManagedPortPool)(poolWriter, poolReq)
	require.Equal(t, http.StatusOK, poolWriter.Code, poolWriter.Body.String())

	var pool v2EnvelopeForTest[struct {
		SuggestedTCP int                             `json:"suggestedTCP"`
		Allocated    []managedPortAllocationResponse `json:"allocated"`
	}]
	require.NoError(t, json.Unmarshal(poolWriter.Body.Bytes(), &pool))
	require.Empty(t, pool.Data.Allocated)
	require.Equal(t, 16000, pool.Data.SuggestedTCP)
}
