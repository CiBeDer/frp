import { buildQueryString, http } from './http'
import type { V2Page } from './http'
import type {
  ClientInfoData,
  ClientListV2Params,
  ManagedClient,
  ManagedClientInput,
  ManagedClientList,
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
