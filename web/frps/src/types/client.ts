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
  enabled?: boolean
  online?: boolean
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
  enabled: boolean
  key?: string
}

export interface ManagedClientList {
  enabled: boolean
  items: ManagedClient[]
}

export interface ManagedPortRange {
  start: number
  end: number
}

export interface ManagedPortAllocation {
  type: 'tcp' | 'udp'
  port: number
  clientID: string
  clientName: string
  proxyName: string
  enabled: boolean
  online: boolean
}

export interface ManagedPortPool {
  unrestricted: boolean
  allowed: ManagedPortRange[]
  allocated: ManagedPortAllocation[]
  reservedTCP: number[]
  reservedUDP: number[]
  suggestedTCP: number
  suggestedUDP: number
}

export interface ManagedEvent {
  id: number
  time: string
  type: string
  level: 'info' | 'warning' | 'error' | string
  clientID?: string
  clientName?: string
  proxyName?: string
  message: string
}

export interface ManagedBackupClient {
  id: string
  name: string
  token: string
  serverAddr: string
  proxies: ManagedProxy[]
  enabled?: boolean
}

export interface ManagedBackup {
  version: number
  clients: ManagedBackupClient[]
}
