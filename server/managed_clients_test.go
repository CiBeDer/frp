package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fatedier/frp/pkg/auth"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/proto/wire"
	"github.com/fatedier/frp/server/managed"
)

func TestManagedClientSessionAuthorization(t *testing.T) {
	svr := newControlTestService(t)
	svr.cfg.Auth.AdditionalScopes = []v1.AuthScope{v1.AuthScopeHeartBeats, v1.AuthScopeNewWorkConns}
	store, err := managed.NewStore(filepath.Join(t.TempDir(), "clients.json"), nil)
	require.NoError(t, err)
	svr.managedClients = store
	client, err := store.Create(managed.Client{
		Name: "first", Token: "client-specific-key", ServerAddr: "127.0.0.1",
		Proxies: []managed.Proxy{{Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: 16001}},
	})
	require.NoError(t, err)
	other, err := store.Create(managed.Client{
		Name: "second", Token: "another-client-key", ServerAddr: "127.0.0.1",
		Proxies: []managed.Proxy{{Name: "ssh", Type: "tcp", LocalPort: 22, RemotePort: 16002}},
	})
	require.NoError(t, err)
	register := func(token, runID string) (*Control, error) {
		conn := newDeadlineReadConn()
		t.Cleanup(func() { _ = conn.Close() })
		login := &msg.Login{Timestamp: time.Now().Unix(), RunID: runID, User: other.ID, ClientID: other.ID}
		require.NoError(t, auth.NewTokenAuth(nil, token).SetLogin(login))
		return svr.RegisterControl(msg.NewConn(conn, msg.NewV1ReadWriter(conn)), login, false, wire.ProtocolV1, "")
	}
	_, err = register("", "global-token") // Original server uses the empty global token.
	require.Error(t, err)
	_, err = register("wrong-key", "wrong-token")
	require.Error(t, err)
	ctl, err := register(client.Token, "first-run")
	require.NoError(t, err)
	t.Cleanup(func() { svr.ctlManager.Remove(ctl); _ = ctl.Close() })
	require.Equal(t, client.ID, ctl.sessionCtx.ManagedClientID)
	require.Equal(t, client.ID, ctl.sessionCtx.LoginMsg.User)
	require.Equal(t, client.ID, ctl.sessionCtx.LoginMsg.ClientID)
	require.Equal(t, []byte(client.Token), ctl.sessionCtx.EncryptionKey)

	_, err = register(other.Token, "first-run")
	require.ErrorContains(t, err, "another client")
	require.Same(t, ctl, currentControlForTest(svr.ctlManager, "first-run"))
	for _, proxyMsg := range []*msg.NewProxy{
		{ProxyType: "tcp", RemotePort: 16002},
		{ProxyType: "tcp", RemotePort: 0},
		{ProxyType: "udp", RemotePort: 16001},
		{ProxyType: "http"},
		{ProxyType: "tcp", RemotePort: 16001, Group: "shared"},
	} {
		_, err = ctl.RegisterProxy(proxyMsg)
		require.Error(t, err)
	}
	setter := auth.NewTokenAuth(svr.cfg.Auth.AdditionalScopes, client.Token)
	ping := &msg.Ping{}
	require.NoError(t, setter.SetPing(ping))
	require.NoError(t, ctl.sessionCtx.AuthVerifier.VerifyPing(ping))
	work := &msg.NewWorkConn{}
	require.NoError(t, setter.SetNewWorkConn(work))
	require.NoError(t, ctl.sessionCtx.AuthVerifier.VerifyNewWorkConn(work))
	require.NoError(t, auth.NewTokenAuth(svr.cfg.Auth.AdditionalScopes, other.Token).SetPing(ping))
	require.Error(t, ctl.sessionCtx.AuthVerifier.VerifyPing(ping))
	require.NoError(t, auth.NewTokenAuth(svr.cfg.Auth.AdditionalScopes, other.Token).SetNewWorkConn(work))
	require.Error(t, ctl.sessionCtx.AuthVerifier.VerifyNewWorkConn(work))
}
