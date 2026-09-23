// Copyright 2026 The frp Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	httppkg "github.com/fatedier/frp/pkg/util/http"
	"github.com/fatedier/frp/server/managed"
)

type managedClientResponse struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	ServerAddr string          `json:"serverAddr"`
	Proxies    []managed.Proxy `json:"proxies"`
	Online     bool            `json:"online"`
	Key        string          `json:"key,omitempty"`
}

// SetManagedClients attaches the same store used by connection authorization.
func (c *Controller) SetManagedClients(store *managed.Store) {
	c.managedClients = store
}

func (c *Controller) managedClientResponse(client managed.Client) managedClientResponse {
	item := managedClientResponse{
		ID: client.ID, Name: client.Name, ServerAddr: client.ServerAddr, Proxies: client.Proxies,
	}
	if c.clientRegistry != nil {
		if info, ok := c.clientRegistry.GetByKey(client.ID + "." + client.ID); ok {
			item.Key = info.Key
			item.Online = info.Online
		}
	}
	return item
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
	}{Enabled: c.managedClients != nil, Items: items}, nil
}

func (c *Controller) APIV2ManagedClientCreate(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if c.managedClients == nil {
		return nil, httppkg.NewError(http.StatusConflict, "enable clientManagement in frps configuration first")
	}
	// JSON-only requests also prevent cross-site form submissions using dashboard credentials.
	mediaType, _, err := mime.ParseMediaType(ctx.Req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, httppkg.NewError(http.StatusUnsupportedMediaType, "Content-Type must be application/json")
	}
	var input struct {
		Name       string          `json:"name"`
		Token      string          `json:"token"`
		ServerAddr string          `json:"serverAddr"`
		Proxies    []managed.Proxy `json:"proxies"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Resp, ctx.Req.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, httppkg.NewError(http.StatusBadRequest, "invalid client JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, httppkg.NewError(http.StatusBadRequest, "expected one JSON object")
	}
	for _, proxy := range input.Proxies {
		reserved := []int{c.serverCfg.BindPort, c.serverCfg.WebServer.Port, c.serverCfg.VhostHTTPPort,
			c.serverCfg.VhostHTTPSPort, c.serverCfg.TCPMuxHTTPConnectPort, c.serverCfg.SSHTunnelGateway.BindPort}
		if proxy.Type == "udp" {
			reserved = []int{c.serverCfg.KCPBindPort, c.serverCfg.QUICBindPort}
		}
		for _, port := range reserved {
			if port > 0 && port == proxy.RemotePort {
				return nil, httppkg.NewError(http.StatusConflict, "remote port is reserved by a server listener")
			}
		}
	}
	client, err := c.managedClients.Create(managed.Client{
		Name: input.Name, Token: input.Token, ServerAddr: input.ServerAddr, Proxies: input.Proxies,
	})
	if err != nil {
		return nil, managedHTTPError(err)
	}
	return c.managedClientResponse(client), nil
}

func (c *Controller) APIV2ManagedClientConfig(ctx *httppkg.Context) (any, error) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	if c.managedClients == nil {
		return nil, httppkg.NewError(http.StatusConflict, "enable clientManagement in frps configuration first")
	}
	config, err := c.managedClients.TOML(ctx.Param("id"), c.serverCfg.BindPort, c.serverCfg.Auth.AdditionalScopes)
	if err != nil {
		return nil, managedHTTPError(err)
	}
	return struct {
		TOML string `json:"toml"`
	}{TOML: config}, nil
}

func managedHTTPError(err error) error {
	switch {
	case errors.Is(err, managed.ErrInvalid):
		return httppkg.NewError(http.StatusBadRequest, err.Error())
	case errors.Is(err, managed.ErrConflict):
		return httppkg.NewError(http.StatusConflict, err.Error())
	case errors.Is(err, managed.ErrNotFound):
		return httppkg.NewError(http.StatusNotFound, "managed client not found")
	default:
		return httppkg.NewError(http.StatusInternalServerError, "could not persist client configuration; check the store path and permissions")
	}
}
