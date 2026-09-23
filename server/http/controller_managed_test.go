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

	v1 "github.com/fatedier/frp/pkg/config/v1"
	httppkg "github.com/fatedier/frp/pkg/util/http"
	"github.com/fatedier/frp/server/managed"
	"github.com/fatedier/frp/server/registry"
)

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
	body := `{"name":"Office","token":"never-list-this-secret","serverAddr":"127.0.0.1","proxies":[{"name":"ssh","type":"tcp","localIP":"127.0.0.1","localPort":22,"remotePort":16001},{"name":"dns","type":"udp","localIP":"127.0.0.1","localPort":53,"remotePort":16001}]}`
	require.Equal(t, http.StatusUnsupportedMediaType, request(c.APIV2ManagedClientCreate, body, "text/plain").Code)
	require.Equal(t, http.StatusBadRequest, request(c.APIV2ManagedClientCreate, body+`{}`, "application/json").Code)
	w = request(c.APIV2ManagedClientCreate, body, "application/json")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "never-list-this-secret")
	var created v2EnvelopeForTest[managedClientResponse]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.Data.ID)
	require.Len(t, created.Data.Proxies, 2)
	require.Equal(t, http.StatusConflict, request(c.APIV2ManagedClientCreate, body, "application/json").Code)
	require.Equal(t, http.StatusConflict, request(c.APIV2ManagedClientCreate,
		strings.ReplaceAll(body, "16001", "7000"), "application/json").Code)

	c.clientRegistry.Register(created.Data.ID, created.Data.ID, "run", "pc", "test", "127.0.0.1", "v1")
	w = request(c.APIV2ManagedClientList, "", "")
	require.Contains(t, w.Body.String(), `"online":true`)
	require.NotContains(t, w.Body.String(), "never-list-this-secret")
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
