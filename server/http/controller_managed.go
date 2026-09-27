// Copyright 2026 The frp Authors
// Licensed under the Apache License, Version 2.0 (the "License").

package http

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"

	"github.com/fatedier/frp/pkg/naming"
	httppkg "github.com/fatedier/frp/pkg/util/http"
	"github.com/fatedier/frp/server/managed"
)

type ManagedClientRuntime interface {
	DisconnectManagedClient(clientID string)
	DisconnectAllManagedClients()
}

type managedProxyResponse struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	LocalIP    string `json:"localIP"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
	Enabled    bool   `json:"enabled"`
	Online     bool   `json:"online"`
}

type managedClientResponse struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	ServerAddr string                 `json:"serverAddr"`
	Proxies    []managedProxyResponse `json:"proxies"`
	Online     bool                   `json:"online"`
	Enabled    bool                   `json:"enabled"`
	Key        string                 `json:"key,omitempty"`
}

type managedPortRangeResponse struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type managedPortAllocationResponse struct {
	Type       string `json:"type"`
	Port       int    `json:"port"`
	ClientID   string `json:"clientID"`
	ClientName string `json:"clientName"`
	ProxyName  string `json:"proxyName"`
	Enabled    bool   `json:"enabled"`
	Online     bool   `json:"online"`
}

func (c *Controller) SetManagedClients(store *managed.Store) {
	c.managedClients = store
}

func (c *Controller) SetManagedRuntime(runtime ManagedClientRuntime) {
	c.managedRuntime = runtime
}

func (c *Controller) managedProxyOnline(client managed.Client, proxy managed.Proxy) bool {
	if !client.IsEnabled() || !proxy.IsEnabled() || c.pxyManager == nil {
		return false
	}
	_, ok := c.pxyManager.GetByName(naming.AddUserPrefix(client.ID, proxy.Name))
	return ok
}

func (c *Controller) managedProxyResponse(client managed.Client, proxy managed.Proxy) managedProxyResponse {
	return managedProxyResponse{
		Name:       proxy.Name,
		Type:       proxy.Type,
		LocalIP:    proxy.LocalIP,
		LocalPort:  proxy.LocalPort,
		RemotePort: proxy.RemotePort,
		Enabled:    proxy.IsEnabled(),
		Online:     c.managedProxyOnline(client, proxy),
	}
}

func (c *Controller) managedClientResponse(client managed.Client) managedClientResponse {
	proxies := make([]managedProxyResponse, 0, len(client.Proxies))
	for _, proxy := range client.Proxies {
		proxies = append(proxies, c.managedProxyResponse(client, proxy))
	}
	item := managedClientResponse{
		ID:         client.ID,
		Name:       client.Name,
		ServerAddr: client.ServerAddr,
		Proxies:    proxies,
		Enabled:    client.IsEnabled(),
	}
	if c.clientRegistry != nil {
		if info, ok := c.clientRegistry.GetByKey(client.ID + "." + client.ID); ok {
			item.Key = info.Key
			item.Online = info.Online
		}
	}
	return item
}

func requireManagedJSON(ctx *httppkg.Context) error {
	mediaType, _, err := mime.ParseMediaType(ctx.Req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return httppkg.NewError(http.StatusUnsupportedMediaType, "Content-Type must be application/json")
	}
	return nil
}

func decodeManagedJSON(ctx *httppkg.Context, dst any, limit int64) error {
	if err := requireManagedJSON(ctx); err != nil {
		return err
	}
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return httppkg.NewError(http.StatusBadRequest, "invalid JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return httppkg.NewError(http.StatusBadRequest, "expected one JSON object")
	}
	return nil
}

func (c *Controller) requireManagedStore() error {
	if c.managedClients == nil {
		return httppkg.NewError(http.StatusConflict, "enable clientManagement in frps configuration first")
	}
	return nil
}

func (c *Controller) validateManagedReservedPorts(proxies []managed.Proxy) error {
	for _, proxy := range proxies {
		reserved := []int{
			c.serverCfg.BindPort,
			c.serverCfg.WebServer.Port,
			c.serverCfg.VhostHTTPPort,
			c.serverCfg.VhostHTTPSPort,
			c.serverCfg.TCPMuxHTTPConnectPort,
			c.serverCfg.SSHTunnelGateway.BindPort,
		}
		if proxy.Type == "udp" {
			reserved = []int{c.serverCfg.KCPBindPort, c.serverCfg.QUICBindPort}
		}
		for _, port := range reserved {
			if port > 0 && port == proxy.RemotePort {
				return httppkg.NewError(http.StatusConflict, "remote port is reserved by a server listener")
			}
		}
	}
	return nil
}

func (c *Controller) APIV2ManagedClientList(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	items := make([]managedClientResponse, 0)
	if c.managedClients != nil {
		for _, client := range c.managedClients.List() {
			items = append(items, c.managedClientResponse(client))
		}
	}
	return struct {
		Enabled bool                    `json:"enabled"`
		Items   []managedClientResponse `json:"items"`
	}{
		Enabled: c.managedClients != nil,
		Items:   items,
	}, nil
}

func (c *Controller) APIV2ManagedClientCreate(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	var input struct {
		Name       string          `json:"name"`
		Token      string          `json:"token"`
		ServerAddr string          `json:"serverAddr"`
		Proxies    []managed.Proxy `json:"proxies"`
	}
	if err := decodeManagedJSON(ctx, &input, 1<<20); err != nil {
		return nil, err
	}
	if err := c.validateManagedReservedPorts(input.Proxies); err != nil {
		return nil, err
	}
	client, err := c.managedClients.Create(managed.Client{
		Name:       input.Name,
		Token:      input.Token,
		ServerAddr: input.ServerAddr,
		Proxies:    input.Proxies,
	})
	if err != nil {
		return nil, managedHTTPError(err)
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:     "client_created",
		ClientID: client.ID,
		Message:  "Managed client created",
	})
	return c.managedClientResponse(client), nil
}

func (c *Controller) APIV2ManagedClientUpdate(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	id := ctx.Param("id")
	old, ok := c.managedClients.Get(id)
	if !ok {
		return nil, managedHTTPError(managed.ErrNotFound)
	}
	var input struct {
		Name       string          `json:"name"`
		ServerAddr string          `json:"serverAddr"`
		Proxies    []managed.Proxy `json:"proxies"`
	}
	if err := decodeManagedJSON(ctx, &input, 1<<20); err != nil {
		return nil, err
	}
	if err := c.validateManagedReservedPorts(input.Proxies); err != nil {
		return nil, err
	}
	updated, err := c.managedClients.Update(id, managed.Client{
		Name:       input.Name,
		ServerAddr: input.ServerAddr,
		Proxies:    input.Proxies,
	})
	if err != nil {
		return nil, managedHTTPError(err)
	}

	if c.managedRuntime != nil && managedProxyGrantChanged(old.Proxies, updated.Proxies) {
		c.managedRuntime.DisconnectManagedClient(id)
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:     "client_updated",
		ClientID: id,
		Message:  "Client and proxy configuration updated",
	})
	return c.managedClientResponse(updated), nil
}

func (c *Controller) APIV2ManagedClientEnabled(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeManagedJSON(ctx, &input, 64<<10); err != nil {
		return nil, err
	}
	client, err := c.managedClients.SetClientEnabled(ctx.Param("id"), input.Enabled)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	if !input.Enabled && c.managedRuntime != nil {
		c.managedRuntime.DisconnectManagedClient(client.ID)
	}
	action := "enabled"
	if !input.Enabled {
		action = "disabled"
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:     "client_" + action,
		ClientID: client.ID,
		Message:  "Client " + action,
	})
	return c.managedClientResponse(client), nil
}

func (c *Controller) APIV2ManagedProxyEnabled(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	var input struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeManagedJSON(ctx, &input, 64<<10); err != nil {
		return nil, err
	}
	client, err := c.managedClients.SetProxyEnabled(ctx.Param("id"), input.Name, input.Enabled)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	if c.managedRuntime != nil {
		// Official frpc has no server-pushed "stop one proxy" control message.
		// Recycle the control connection so the client reconnects and the
		// managed authorization decision is applied immediately.
		c.managedRuntime.DisconnectManagedClient(client.ID)
	}
	action := "enabled"
	if !input.Enabled {
		action = "disabled"
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:      "proxy_" + action,
		ClientID:  client.ID,
		ProxyName: input.Name,
		Message:   "Proxy " + action,
	})
	return c.managedClientResponse(client), nil
}

func (c *Controller) APIV2ManagedClientDelete(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	client, err := c.managedClients.Delete(ctx.Param("id"))
	if err != nil {
		return nil, managedHTTPError(err)
	}
	if c.managedRuntime != nil {
		c.managedRuntime.DisconnectManagedClient(client.ID)
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:       "client_deleted",
		ClientID:   client.ID,
		ClientName: client.Name,
		Level:      "warning",
		Message:    "Managed client deleted",
	})
	return struct {
		ID string `json:"id"`
	}{ID: client.ID}, nil
}

func (c *Controller) APIV2ManagedClientRotateToken(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	client, err := c.managedClients.RotateToken(ctx.Param("id"))
	if err != nil {
		return nil, managedHTTPError(err)
	}
	if c.managedRuntime != nil {
		c.managedRuntime.DisconnectManagedClient(client.ID)
	}
	config, err := c.managedClients.TOML(
		client.ID,
		c.serverCfg.BindPort,
		c.serverCfg.Auth.AdditionalScopes,
	)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:     "token_rotated",
		ClientID: client.ID,
		Level:    "warning",
		Message:  "Client key rotated; previous key is no longer valid",
	})
	return struct {
		Client managedClientResponse `json:"client"`
		TOML   string                `json:"toml"`
	}{
		Client: c.managedClientResponse(client),
		TOML:   config,
	}, nil
}

func (c *Controller) APIV2ManagedClientConfig(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	config, err := c.managedClients.TOML(
		ctx.Param("id"),
		c.serverCfg.BindPort,
		c.serverCfg.Auth.AdditionalScopes,
	)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	return struct {
		TOML string `json:"toml"`
	}{TOML: config}, nil
}

func (c *Controller) managedReservedPortSet(proxyType string) map[int]bool {
	out := map[int]bool{}
	ports := []int{
		c.serverCfg.BindPort,
		c.serverCfg.WebServer.Port,
		c.serverCfg.VhostHTTPPort,
		c.serverCfg.VhostHTTPSPort,
		c.serverCfg.TCPMuxHTTPConnectPort,
		c.serverCfg.SSHTunnelGateway.BindPort,
	}
	if proxyType == "udp" {
		ports = []int{c.serverCfg.KCPBindPort, c.serverCfg.QUICBindPort}
	}
	for _, port := range ports {
		if port > 0 {
			out[port] = true
		}
	}
	return out
}

func (c *Controller) suggestManagedPort(proxyType string, allocated map[string]bool) int {
	reserved := c.managedReservedPortSet(proxyType)
	tryRange := func(start, end int) int {
		if start < 1 {
			start = 1
		}
		if end > 65535 {
			end = 65535
		}
		for port := start; port <= end; port++ {
			if reserved[port] || allocated[proxyType+":"+strconv.Itoa(port)] {
				continue
			}
			return port
		}
		return 0
	}

	for _, allowed := range c.serverCfg.AllowPorts {
		if allowed.Single > 0 {
			if port := tryRange(allowed.Single, allowed.Single); port > 0 {
				return port
			}
			continue
		}
		if port := tryRange(allowed.Start, allowed.End); port > 0 {
			return port
		}
	}
	if len(c.serverCfg.AllowPorts) == 0 {
		if port := tryRange(6000, 65535); port > 0 {
			return port
		}
		return tryRange(1024, 5999)
	}
	return 0
}

func (c *Controller) APIV2ManagedPortPool(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	allowed := make([]managedPortRangeResponse, 0, len(c.serverCfg.AllowPorts))
	for _, item := range c.serverCfg.AllowPorts {
		if item.Single > 0 {
			allowed = append(allowed, managedPortRangeResponse{
				Start: item.Single,
				End:   item.Single,
			})
		} else {
			allowed = append(allowed, managedPortRangeResponse{
				Start: item.Start,
				End:   item.End,
			})
		}
	}

	allocated := make([]managedPortAllocationResponse, 0)
	used := map[string]bool{}
	for _, client := range c.managedClients.List() {
		for _, proxy := range client.Proxies {
			allocated = append(allocated, managedPortAllocationResponse{
				Type:       proxy.Type,
				Port:       proxy.RemotePort,
				ClientID:   client.ID,
				ClientName: client.Name,
				ProxyName:  proxy.Name,
				Enabled:    client.IsEnabled() && proxy.IsEnabled(),
				Online:     c.managedProxyOnline(client, proxy),
			})
			used[proxy.Type+":"+strconv.Itoa(proxy.RemotePort)] = true
		}
	}
	slices.SortFunc(allocated, func(a, b managedPortAllocationResponse) int {
		if v := cmp.Compare(a.Port, b.Port); v != 0 {
			return v
		}
		return cmp.Compare(a.Type, b.Type)
	})

	return struct {
		Unrestricted bool                            `json:"unrestricted"`
		Allowed      []managedPortRangeResponse      `json:"allowed"`
		Allocated    []managedPortAllocationResponse `json:"allocated"`
		ReservedTCP  []int                           `json:"reservedTCP"`
		ReservedUDP  []int                           `json:"reservedUDP"`
		SuggestedTCP int                             `json:"suggestedTCP"`
		SuggestedUDP int                             `json:"suggestedUDP"`
	}{
		Unrestricted: len(c.serverCfg.AllowPorts) == 0,
		Allowed:      allowed,
		Allocated:    allocated,
		ReservedTCP:  mapsToSortedPorts(c.managedReservedPortSet("tcp")),
		ReservedUDP:  mapsToSortedPorts(c.managedReservedPortSet("udp")),
		SuggestedTCP: c.suggestManagedPort("tcp", used),
		SuggestedUDP: c.suggestManagedPort("udp", used),
	}, nil
}

func mapsToSortedPorts(input map[int]bool) []int {
	out := make([]int, 0, len(input))
	for port := range input {
		out = append(out, port)
	}
	slices.Sort(out)
	return out
}

func managedProxyGrantChanged(before, after []managed.Proxy) bool {
	beforeByName := make(map[string]managed.Proxy, len(before))
	for _, proxy := range before {
		beforeByName[proxy.Name] = proxy
	}
	afterByName := make(map[string]managed.Proxy, len(after))
	for _, proxy := range after {
		afterByName[proxy.Name] = proxy
	}

	for name, oldProxy := range beforeByName {
		newProxy, ok := afterByName[name]
		if !ok ||
			oldProxy.Type != newProxy.Type ||
			oldProxy.RemotePort != newProxy.RemotePort ||
			oldProxy.IsEnabled() != newProxy.IsEnabled() {
			return true
		}
	}
	return false
}

func (c *Controller) APIV2ManagedEvents(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	limit := 50
	if raw := ctx.Query("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 200 {
			limit = value
		}
	}
	return struct {
		Items []managed.Event `json:"items"`
	}{
		Items: c.managedClients.Events(ctx.Query("clientID"), limit),
	}, nil
}

func (c *Controller) APIV2ManagedBackup(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	return c.managedClients.Backup(), nil
}

func (c *Controller) APIV2ManagedRestore(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if err := c.requireManagedStore(); err != nil {
		return nil, err
	}
	var backup managed.Backup
	if err := decodeManagedJSON(ctx, &backup, 4<<20); err != nil {
		return nil, err
	}
	for _, client := range backup.Clients {
		if err := c.validateManagedReservedPorts(client.Proxies); err != nil {
			return nil, err
		}
	}
	clients, err := c.managedClients.Restore(backup)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	if c.managedRuntime != nil {
		c.managedRuntime.DisconnectAllManagedClients()
	}
	c.managedClients.RecordEvent(managed.Event{
		Type:    "backup_restored",
		Level:   "warning",
		Message: "Managed client backup restored; active clients were disconnected",
	})
	items := make([]managedClientResponse, 0, len(clients))
	for _, client := range clients {
		items = append(items, c.managedClientResponse(client))
	}
	return struct {
		Items []managedClientResponse `json:"items"`
	}{Items: items}, nil
}

func managedHTTPError(err error) error {
	switch {
	case errors.Is(err, managed.ErrInvalid):
		return httppkg.NewError(http.StatusBadRequest, err.Error())
	case errors.Is(err, managed.ErrConflict):
		return httppkg.NewError(http.StatusConflict, err.Error())
	case errors.Is(err, managed.ErrDisabled):
		return httppkg.NewError(http.StatusConflict, err.Error())
	case errors.Is(err, managed.ErrNotFound):
		return httppkg.NewError(http.StatusNotFound, "managed client or proxy not found")
	default:
		return httppkg.NewError(
			http.StatusInternalServerError,
			"could not persist client configuration; check the store path and permissions",
		)
	}
}
