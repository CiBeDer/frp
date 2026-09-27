import { buildQueryString, http } from './http'
import type { V2Page } from './http'
import type {
  ClientInfoData,
  ClientListV2Params,
  ManagedBackup,
  ManagedClient,
  ManagedClientInput,
  ManagedClientList,
  ManagedEvent,
  ManagedPortPool,
} from '../types/client'

export const getClients = () => {
  return http.get<ClientInfoData[]>('../api/clients')
}

export const getClientsV2 = (params: ClientListV2Params = {}) => {
  return http.getV2<V2Page<ClientInfoData>>(
    `../api/v2/clients${buildQueryString({
      page: params.page,
      pageSize: params.pageSize,
      status:
        params.status && params.status !== 'all' ? params.status : undefined,
      q: params.q || undefined,
      user: params.user,
      clientID: params.clientID || undefined,
      runID: params.runID || undefined,
    })}`,
  )
}

export const getClient = (key: string) => {
  return http.get<ClientInfoData>(`../api/clients/${key}`)
}

export const getClientV2 = (key: string) => {
  return http.getV2<ClientInfoData>(
    `../api/v2/clients/${encodeURIComponent(key)}`,
  )
}

export const getManagedClients = () => {
  return http.getV2<ManagedClientList>('../api/v2/managed-clients')
}

export const createManagedClient = (input: ManagedClientInput) => {
  return http.postV2<ManagedClient>('../api/v2/managed-clients', input)
}

export const getManagedClientConfig = (id: string) => {
  return http.getV2<{ toml: string }>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}/config`,
  )
}

export const updateManagedClient = (
  id: string,
  input: Omit<ManagedClientInput, 'token'>,
) => {
  return http.putV2<ManagedClient>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}`,
    input,
  )
}

export const setManagedClientEnabled = (id: string, enabled: boolean) => {
  return http.patchV2<ManagedClient>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}/enabled`,
    { enabled },
  )
}

export const setManagedProxyEnabled = (
  id: string,
  name: string,
  enabled: boolean,
) => {
  return http.patchV2<ManagedClient>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}/proxy-enabled`,
    { name, enabled },
  )
}

export const deleteManagedClient = (id: string) => {
  return http.deleteV2<{ id: string }>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}`,
  )
}

export const rotateManagedClientToken = (id: string) => {
  return http.postV2<{ client: ManagedClient; toml: string }>(
    `../api/v2/managed-clients/${encodeURIComponent(id)}/rotate-token`,
  )
}

export const getManagedPortPool = () => {
  return http.getV2<ManagedPortPool>('../api/v2/managed-ports')
}

export const getManagedEvents = (clientID?: string, limit = 50) => {
  return http.getV2<{ items: ManagedEvent[] }>(
    `../api/v2/managed-events${buildQueryString({
      clientID: clientID || undefined,
      limit,
    })}`,
  )
}

export const getManagedBackup = () => {
  return http.getV2<ManagedBackup>('../api/v2/managed-clients/backup')
}

export const restoreManagedBackup = (backup: ManagedBackup) => {
  return http.postV2<{ items: ManagedClient[] }>(
    '../api/v2/managed-clients/restore',
    backup,
  )
}
