export interface ClientInfoData {
  key: string
  user: string
  clientID: string
  runID: string
  version?: string
  wireProtocol?: string
  hostname: string
  clientIP?: string
  firstConnectedAt: number
  lastConnectedAt: number
  disconnectedAt?: number
  online: boolean
  status?: ClientStatus
}

export interface ClientStatus {
  phase: 'online' | 'offline'
  curConns: number
  proxyCount: number
}

export interface ClientListV2Params {
  page?: number
  pageSize?: number
  status?: 'all' | 'online' | 'offline'
  q?: string
  user?: string
  clientID?: string
  runID?: string
}

export interface ManagedProxy {
  name: string
  type: 'tcp' | 'udp'
  localIP: string
  localPort: number
  remotePort: number
}

export interface ManagedClientInput {
  name: string
  token?: string
  serverAddr: string
  proxies: ManagedProxy[]
}

export interface ManagedClient {
  id: string
  name: string
  serverAddr: string
  proxies: ManagedProxy[]
  online: boolean
  key?: string
}

export interface ManagedClientList {
  enabled: boolean
  items: ManagedClient[]
}
