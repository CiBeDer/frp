<template>
  <section class="managed-clients" aria-labelledby="managed-clients-title">
    <div class="section-header">
      <div>
        <h2 id="managed-clients-title">Managed clients</h2>
        <p>
          Manage client keys, proxy assignments, port allocation, diagnostics,
          and deployable frpc.toml files.
        </p>
      </div>
      <div class="header-actions">
        <el-button v-if="enabled" :icon="Connection" @click="openPortPool">
          Port pool
        </el-button>
        <el-button v-if="enabled" :icon="Download" @click="downloadBackup">
          Backup
        </el-button>
        <el-button v-if="enabled" :icon="Upload" @click="triggerRestore">
          Restore
        </el-button>
        <el-button
          v-if="enabled"
          type="primary"
          :icon="Plus"
          @click="openCreateDialog"
        >
          Add client
        </el-button>
      </div>
    </div>

    <input
      ref="restoreInput"
      class="hidden-file-input"
      type="file"
      accept=".json,application/json"
      @change="restoreBackupFile"
    />

    <el-alert v-if="loadError" type="error" :closable="false" show-icon>
      <template #title>Failed to load managed clients: {{ loadError }}</template>
      <el-button link type="primary" @click="refreshAll()">Retry</el-button>
    </el-alert>

    <div v-loading="loading" class="managed-content">
      <el-alert
        v-if="loaded && !enabled"
        title="Enable client management in frps.toml"
        type="info"
        :closable="false"
        show-icon
      >
        <p>
          Enable clientManagement and configure dashboard credentials. Once
          enabled, only registered client keys can connect.
        </p>
        <pre class="enable-config">
auth.method = "token"
clientManagement.enabled = true
clientManagement.storePath = "./frps-clients.json"</pre>
      </el-alert>

      <template v-else-if="enabled">
        <el-empty
          v-if="clients.length === 0 && !loading"
          description="No managed clients yet. Add one to assign ports."
          :image-size="72"
        />

        <article
          v-for="client in clients"
          :key="client.id"
          class="managed-card"
          :class="{ disabled: !client.enabled }"
        >
          <div class="managed-card-header">
            <div class="client-heading">
              <h3>{{ client.name }}</h3>
              <el-tag
                :type="
                  !client.enabled
                    ? 'warning'
                    : client.online
                      ? 'success'
                      : 'info'
                "
                size="small"
              >
                {{
                  !client.enabled
                    ? 'Disabled'
                    : client.online
                      ? 'Online'
                      : 'Offline'
                }}
              </el-tag>
              <span class="proxy-count">
                {{ client.proxies.length }}
                {{ client.proxies.length === 1 ? 'proxy' : 'proxies' }}
              </span>
            </div>

            <div class="client-actions">
              <span class="switch-label">Client</span>
              <el-switch
                :model-value="client.enabled"
                :loading="actionBusy === `client:${client.id}`"
                @change="toggleClient(client, $event)"
              />
              <router-link
                v-if="client.key"
                :to="{ name: 'ClientDetail', params: { key: client.key } }"
              >
                <el-button link type="primary">Connection details</el-button>
              </router-link>
              <el-button :icon="Edit" @click="openEditDialog(client)">
                Edit
              </el-button>
              <el-button
                :icon="CopyDocument"
                :loading="configLoadingID === client.id"
                @click="showConfig(client, 'copy')"
              >
                Copy TOML
              </el-button>
              <el-button
                :icon="Download"
                :loading="configLoadingID === client.id"
                @click="showConfig(client, 'download')"
              >
                Download
              </el-button>
              <el-button :icon="Key" @click="rotateToken(client)">
                Reset key
              </el-button>
              <el-button
                type="danger"
                plain
                :icon="Delete"
                @click="removeClient(client)"
              >
                Delete
              </el-button>
            </div>
          </div>

          <p class="server-address">
            <strong>Server:</strong> {{ client.serverAddr }}
            <span class="client-id">ID: {{ client.id }}</span>
          </p>

          <div v-if="client.proxies.length > 0" class="client-ports">
            <span class="client-ports-label">Ports:</span>
            <el-tooltip
              placement="top-start"
              :show-after="200"
              popper-class="managed-port-tooltip"
            >
              <template #content>
                <div class="port-tooltip">
                  <div
                    v-for="proxy in client.proxies"
                    :key="`tooltip:${proxy.name}`"
                    class="port-tooltip-row"
                  >
                    <span
                      class="port-status-dot"
                      :class="proxy.online ? 'online' : 'offline'"
                    ></span>
                    <strong>{{ proxy.type.toUpperCase() }} {{ proxy.remotePort }}</strong>
                    <span>{{ proxy.name }}</span>
                    <span>
                      {{ proxy.online ? 'Online' : 'Offline' }}
                      · {{ proxy.localIP }}:{{ proxy.localPort }}
                    </span>
                  </div>
                </div>
              </template>
              <div class="client-port-strip">
                <el-tag
                  v-for="proxy in client.proxies"
                  :key="`port:${proxy.name}`"
                  :type="proxy.online ? 'success' : 'danger'"
                  size="small"
                  effect="light"
                  class="port-chip"
                >
                  {{ proxy.type.toUpperCase() }} {{ proxy.remotePort }}
                </el-tag>
              </div>
            </el-tooltip>
          </div>

          <div v-if="client.proxies.length > 0" class="proxy-list">
            <div
              v-for="proxy in client.proxies"
              :key="proxy.name"
              class="proxy"
              :class="{ disabled: proxy.enabled === false }"
            >
              <el-tag size="small" type="info">
                {{ proxy.type.toUpperCase() }}
              </el-tag>
              <span class="proxy-name">{{ proxy.name }}</span>
              <span class="proxy-route">
                Remote {{ proxy.remotePort }}
                <span aria-label="forwards to">→</span>
                {{ proxy.localIP }}:{{ proxy.localPort }}
              </span>
              <span class="proxy-spacer"></span>
              <el-switch
                :model-value="proxy.enabled !== false"
                :loading="
                  actionBusy === `proxy:${client.id}:${proxy.name}`
                "
                @change="toggleProxy(client, proxy, $event)"
              />
            </div>
          </div>
          <p v-else class="empty-proxies">
            No proxies assigned. Edit this client to add one.
          </p>
        </article>

        <section class="events-card" aria-labelledby="managed-events-title">
          <div class="section-header compact">
            <div>
              <h3 id="managed-events-title">Recent events</h3>
              <p>
                Authentication failures, rejected proxies, startup failures,
                connections, and management actions. Events reset on frps
                restart.
              </p>
            </div>
            <el-button :icon="Refresh" :loading="eventsLoading" @click="fetchEvents">
              Refresh
            </el-button>
          </div>
          <el-empty
            v-if="events.length === 0 && !eventsLoading"
            description="No recent managed-client events"
            :image-size="56"
          />
          <div v-else class="event-list">
            <div v-for="event in events" :key="event.id" class="event-row">
              <span class="event-time">{{ formatEventTime(event.time) }}</span>
              <el-tag :type="eventTagType(event.level)" size="small">
                {{ event.type }}
              </el-tag>
              <span class="event-subject">
                {{ event.clientName || event.clientID || 'Server' }}
                <template v-if="event.proxyName"> / {{ event.proxyName }}</template>
              </span>
              <span class="event-message">{{ event.message }}</span>
            </div>
          </div>
        </section>
      </template>
    </div>

    <el-dialog
      v-model="editorVisible"
      :title="editingClientID ? 'Edit client' : 'Add client'"
      width="min(900px, 96vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @closed="resetEditor"
    >
      <el-alert
        v-if="editingClientID"
        class="editor-note"
        type="info"
        :closable="false"
        show-icon
        title="Client-side changes require redeploying frpc.toml"
      >
        Changes to server address, proxy name, local IP, or local port are saved
        here for the generated configuration. Download the new frpc.toml and
        restart frpc to apply those client-side values.
      </el-alert>

      <el-form label-position="top" :disabled="saving" @submit.prevent="saveClient">
        <div class="client-fields">
          <el-form-item label="Client name" required>
            <el-input
              v-model="form.name"
              placeholder="Office workstation"
              maxlength="100"
            />
          </el-form-item>
          <el-form-item label="Server address" required>
            <el-input v-model="form.serverAddr" placeholder="frp.example.com" />
          </el-form-item>
        </div>

        <el-form-item v-if="!editingClientID" label="Client key (auth.token)">
          <el-input
            v-model="form.token"
            type="password"
            show-password
            autocomplete="new-password"
            placeholder="Leave blank to generate a secure key"
          />
        </el-form-item>

        <div class="section-header proxies-heading">
          <div>
            <h3>Proxies</h3>
            <p>
              Add, remove, edit, or disable proxies. Disabled proxies keep their
              port reservation but are omitted from generated frpc.toml.
            </p>
          </div>
          <el-button :icon="Plus" :disabled="form.proxies.length >= 100" @click="addProxy">
            Add proxy
          </el-button>
        </div>

        <div
          v-for="(proxy, index) in form.proxies"
          :key="proxy.rowID"
          class="proxy-editor"
        >
          <div class="proxy-editor-header">
            <div class="proxy-editor-title">
              <strong>Proxy {{ index + 1 }}</strong>
              <el-switch v-model="proxy.enabled" active-text="Enabled" />
            </div>
            <el-button type="danger" link @click="removeProxy(index)">
              Remove
            </el-button>
          </div>

          <div class="proxy-fields">
            <el-form-item label="Name" required>
              <el-input v-model="proxy.name" placeholder="ssh" />
            </el-form-item>
            <el-form-item label="Type" required>
              <el-select
                v-model="proxy.type"
                aria-label="Proxy type"
                @change="assignSuggestedPort(proxy)"
              >
                <el-option label="TCP" value="tcp" />
                <el-option label="UDP" value="udp" />
              </el-select>
            </el-form-item>
            <el-form-item label="Local IP / hostname" required>
              <el-input v-model="proxy.localIP" placeholder="127.0.0.1" />
            </el-form-item>
            <el-form-item label="Local port" required>
              <el-input-number
                v-model="proxy.localPort"
                :min="1"
                :max="65535"
                :precision="0"
                controls-position="right"
              />
            </el-form-item>
            <el-form-item label="Remote port" required>
              <div class="port-field">
                <el-input-number
                  v-model="proxy.remotePort"
                  :min="1"
                  :max="65535"
                  :precision="0"
                  controls-position="right"
                />
                <el-button @click="assignSuggestedPort(proxy)">Use free</el-button>
              </div>
            </el-form-item>
          </div>
        </div>

        <el-alert
          v-if="saveError"
          :title="saveError"
          type="error"
          :closable="false"
          show-icon
        />
      </el-form>

      <template #footer>
        <el-button :disabled="saving" @click="editorVisible = false">Cancel</el-button>
        <el-button type="primary" :loading="saving" @click="saveClient">
          {{ editingClientID ? 'Save changes' : 'Create client' }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="configVisible"
      :title="`${configClientName} — frpc.toml`"
      width="min(760px, 94vw)"
      @closed="configToml = ''"
    >
      <p class="config-help">
        This file contains the client key. Save it as frpc.toml and run
        <code>frpc -c frpc.toml</code>.
      </p>
      <textarea
        ref="configTextarea"
        class="config-preview"
        :value="configToml"
        aria-label="Client TOML configuration"
        readonly
        spellcheck="false"
        @focus="configTextarea?.select()"
      ></textarea>
      <template #footer>
        <el-button @click="configVisible = false">Close</el-button>
        <el-button :icon="CopyDocument" @click="copyPreview">Copy</el-button>
        <el-button type="primary" :icon="Download" @click="downloadPreview">
          Download frpc.toml
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="portPoolVisible"
      title="Managed port pool"
      width="min(900px, 96vw)"
    >
      <div v-if="portPool" class="port-pool">
        <div class="port-summary">
          <div>
            <span>Allowed</span>
            <strong>{{ allowedRangeText }}</strong>
          </div>
          <div>
            <span>Suggested TCP</span>
            <strong>{{ portPool.suggestedTCP || 'None' }}</strong>
          </div>
          <div>
            <span>Suggested UDP</span>
            <strong>{{ portPool.suggestedUDP || 'None' }}</strong>
          </div>
        </div>

        <p class="port-note">
          Disabled clients and proxies keep their assigned ports reserved. Server
          listener ports are excluded from suggestions.
        </p>

        <div class="table-scroll">
          <table class="port-table">
            <thead>
              <tr>
                <th>Protocol</th>
                <th>Port</th>
                <th>Client</th>
                <th>Proxy</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in portPool.allocated" :key="`${item.type}:${item.port}`">
                <td>{{ item.type.toUpperCase() }}</td>
                <td>{{ item.port }}</td>
                <td>{{ item.clientName }}</td>
                <td>{{ item.proxyName }}</td>
                <td>
                  <el-tag :type="item.online ? 'success' : 'danger'" size="small">
                    {{ item.online ? 'Online' : 'Offline' }}
                  </el-tag>
                </td>
              </tr>
              <tr v-if="portPool.allocated.length === 0">
                <td colspan="5" class="empty-cell">No ports allocated</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <div v-else v-loading="portPoolLoading" class="port-loading"></div>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import {
  computed,
  nextTick,
  onMounted,
  onUnmounted,
  reactive,
  ref,
} from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Connection,
  CopyDocument,
  Delete,
  Download,
  Edit,
  Key,
  Plus,
  Refresh,
  Upload,
} from '@element-plus/icons-vue'
import {
  createManagedClient,
  deleteManagedClient,
  getManagedBackup,
  getManagedClientConfig,
  getManagedClients,
  getManagedEvents,
  getManagedPortPool,
  restoreManagedBackup,
  rotateManagedClientToken,
  setManagedClientEnabled,
  setManagedProxyEnabled,
  updateManagedClient,
} from '../api/client'
import type {
  ManagedBackup,
  ManagedClient,
  ManagedEvent,
  ManagedPortPool,
  ManagedProxy,
} from '../types/client'

interface ProxyDraft extends ManagedProxy {
  rowID: number
  enabled: boolean
}

const clients = ref<ManagedClient[]>([])
const events = ref<ManagedEvent[]>([])
const portPool = ref<ManagedPortPool | null>(null)
const enabled = ref(false)
const loaded = ref(false)
const loading = ref(true)
const loadError = ref('')
const eventsLoading = ref(false)
const portPoolLoading = ref(false)
const portPoolVisible = ref(false)
const editorVisible = ref(false)
const editingClientID = ref('')
const saving = ref(false)
const saveError = ref('')
const actionBusy = ref('')
const configLoadingID = ref('')
const configVisible = ref(false)
const configClientName = ref('')
const configToml = ref('')
const configTextarea = ref<HTMLTextAreaElement>()
const restoreInput = ref<HTMLInputElement>()
let nextProxyID = 0
let refreshTimer: number | undefined
let requestSeq = 0

const form = reactive({
  name: '',
  token: '',
  serverAddr: '',
  proxies: [] as ProxyDraft[],
})

const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

const fetchClients = async (silent = false) => {
  const seq = ++requestSeq
  if (!silent) loading.value = true
  try {
    const data = await getManagedClients()
    if (seq !== requestSeq) return
    enabled.value = data.enabled
    clients.value = data.items.map((client) => ({
      ...client,
      enabled: client.enabled !== false,
      proxies: client.proxies.map((proxy) => ({
        ...proxy,
        enabled: proxy.enabled !== false,
      })),
    }))
    loaded.value = true
    loadError.value = ''
  } catch (error) {
    if (seq === requestSeq) loadError.value = errorMessage(error)
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

const fetchEvents = async (silent = false) => {
  if (!enabled.value) return
  eventsLoading.value = true
  try {
    events.value = (await getManagedEvents(undefined, 50)).items
  } catch (error) {
    if (!silent) {
      ElMessage.error('Failed to load recent events: ' + errorMessage(error))
    }
  } finally {
    eventsLoading.value = false
  }
}

const fetchPortPool = async () => {
  if (!enabled.value) return
  portPoolLoading.value = true
  try {
    portPool.value = await getManagedPortPool()
  } catch (error) {
    ElMessage.error('Failed to load port pool: ' + errorMessage(error))
  } finally {
    portPoolLoading.value = false
  }
}

const refreshAll = async (silent = false) => {
  await fetchClients(silent)
  if (enabled.value) {
    await Promise.all([fetchPortPool(), fetchEvents(silent)])
  }
}

const allowedRangeText = computed(() => {
  const pool = portPool.value
  if (!pool) return ''
  if (pool.unrestricted) return '1–65535 (unrestricted)'
  return pool.allowed
    .map((range) =>
      range.start === range.end ? String(range.start) : `${range.start}–${range.end}`,
    )
    .join(', ')
})

const proxyKey = (type: 'tcp' | 'udp', port: number) => `${type}:${port}`

const findFreePort = (type: 'tcp' | 'udp') => {
  const pool = portPool.value
  const used = new Set<string>()
  for (const item of pool?.allocated || []) {
    used.add(proxyKey(item.type, item.port))
  }
  for (const proxy of form.proxies) {
    used.add(proxyKey(proxy.type, proxy.remotePort))
  }
  const reserved = new Set(
    type === 'tcp' ? pool?.reservedTCP || [] : pool?.reservedUDP || [],
  )
  const free = (port: number) =>
    port >= 1 &&
    port <= 65535 &&
    !reserved.has(port) &&
    !used.has(proxyKey(type, port))

  const suggested =
    type === 'tcp' ? pool?.suggestedTCP || 0 : pool?.suggestedUDP || 0
  if (suggested && free(suggested)) return suggested

  if (pool && !pool.unrestricted) {
    for (const range of pool.allowed) {
      for (let port = range.start; port <= range.end; port++) {
        if (free(port)) return port
      }
    }
    return 0
  }

  for (let port = 6000; port <= 65535; port++) {
    if (free(port)) return port
  }
  for (let port = 1024; port < 6000; port++) {
    if (free(port)) return port
  }
  return 0
}

const addProxy = () => {
  const rowID = ++nextProxyID
  const type: 'tcp' | 'udp' = 'tcp'
  form.proxies.push({
    rowID,
    name: `proxy-${rowID}`,
    type,
    localIP: '127.0.0.1',
    localPort: 80,
    remotePort: findFreePort(type) || 6000,
    enabled: true,
  })
}

const removeProxy = (index: number) => {
  form.proxies.splice(index, 1)
}

const assignSuggestedPort = (proxy: ProxyDraft) => {
  const candidate = findFreePort(proxy.type)
  if (candidate) {
    proxy.remotePort = candidate
  } else {
    ElMessage.warning('No free port is available in the configured pool.')
  }
}

const resetEditor = () => {
  editingClientID.value = ''
  form.name = ''
  form.token = ''
  form.serverAddr = ''
  form.proxies = []
  nextProxyID = 0
  saveError.value = ''
}

const openCreateDialog = async () => {
  resetEditor()
  if (!portPool.value) await fetchPortPool()
  form.serverAddr = window.location.hostname.replace(/^\[|\]$/g, '')
  addProxy()
  editorVisible.value = true
}

const openEditDialog = async (client: ManagedClient) => {
  resetEditor()
  if (!portPool.value) await fetchPortPool()
  editingClientID.value = client.id
  form.name = client.name
  form.serverAddr = client.serverAddr
  form.proxies = client.proxies.map((proxy) => ({
    ...proxy,
    rowID: ++nextProxyID,
    enabled: proxy.enabled !== false,
  }))
  editorVisible.value = true
}

const validPort = (value: number) =>
  Number.isInteger(value) && value >= 1 && value <= 65535

const portAllowedByPool = (port: number) => {
  const pool = portPool.value
  if (!pool || pool.unrestricted) return true
  return pool.allowed.some((range) => port >= range.start && port <= range.end)
}

const reservedPortForType = (type: 'tcp' | 'udp', port: number) => {
  const pool = portPool.value
  if (!pool) return false
  const reserved = type === 'tcp' ? pool.reservedTCP : pool.reservedUDP
  return reserved.includes(port)
}

const occupiedByAnotherClient = (type: 'tcp' | 'udp', port: number) => {
  return portPool.value?.allocated.find(
    (item) =>
      item.type === type &&
      item.port === port &&
      item.clientID !== editingClientID.value,
  )
}

const saveClient = async () => {
  if (saving.value) return
  saveError.value = ''
  const name = form.name.trim()
  const serverAddr = form.serverAddr.trim()
  const proxies: ManagedProxy[] = form.proxies.map((proxy) => ({
    name: proxy.name.trim(),
    type: proxy.type,
    localIP: proxy.localIP.trim(),
    localPort: proxy.localPort,
    remotePort: proxy.remotePort,
    enabled: proxy.enabled,
  }))

  if (!name || !serverAddr) {
    saveError.value = 'Enter a client name and server address.'
    return
  }
  if (serverAddr.includes('://') || /\s|\//.test(serverAddr)) {
    saveError.value =
      'Enter a server hostname or IP address without a URL scheme or path.'
    return
  }

  const names = new Set<string>()
  const ports = new Set<string>()
  for (const [index, proxy] of proxies.entries()) {
    if (
      !proxy.name ||
      !proxy.localIP ||
      !validPort(proxy.localPort) ||
      !validPort(proxy.remotePort)
    ) {
      saveError.value = `Proxy ${index + 1}: enter a name, local address, and valid ports.`
      return
    }
    if (names.has(proxy.name)) {
      saveError.value = `Proxy names must be unique: ${proxy.name}.`
      return
    }
    if (!portAllowedByPool(proxy.remotePort)) {
      saveError.value = `Proxy ${index + 1}: remote port ${proxy.remotePort} is outside the server's published port pool.`
      return
    }
    if (reservedPortForType(proxy.type, proxy.remotePort)) {
      saveError.value = `Proxy ${index + 1}: remote port ${proxy.remotePort} is reserved by an frps listener.`
      return
    }
    const occupied = occupiedByAnotherClient(proxy.type, proxy.remotePort)
    if (occupied) {
      saveError.value =
        `Proxy ${index + 1}: ${proxy.type.toUpperCase()} ${proxy.remotePort} is already assigned to ${occupied.clientName} / ${occupied.proxyName}.`
      return
    }
    const key = proxyKey(proxy.type, proxy.remotePort)
    if (ports.has(key)) {
      saveError.value = `Remote port ${proxy.remotePort} is assigned more than once for ${proxy.type.toUpperCase()}.`
      return
    }
    names.add(proxy.name)
    ports.add(key)
  }

  saving.value = true
  try {
    if (editingClientID.value) {
      await updateManagedClient(editingClientID.value, {
        name,
        serverAddr,
        proxies,
      })
      ElMessage.success(
        'Client updated. Re-download frpc.toml if client-side settings changed.',
      )
    } else {
      await createManagedClient({
        name,
        token: form.token || undefined,
        serverAddr,
        proxies,
      })
      ElMessage.success('Client created. Download its frpc.toml to connect.')
    }
    editorVisible.value = false
    await refreshAll()
  } catch (error) {
    saveError.value = errorMessage(error)
  } finally {
    saving.value = false
  }
}

const toggleClient = async (
  client: ManagedClient,
  value: string | number | boolean,
) => {
  const next = Boolean(value)
  const key = `client:${client.id}`
  if (actionBusy.value) return
  actionBusy.value = key
  try {
    await setManagedClientEnabled(client.id, next)
    ElMessage.success(next ? 'Client enabled' : 'Client disabled and disconnected')
    await refreshAll(true)
  } catch (error) {
    ElMessage.error('Failed to change client status: ' + errorMessage(error))
    await fetchClients(true)
  } finally {
    actionBusy.value = ''
  }
}

const toggleProxy = async (
  client: ManagedClient,
  proxy: ManagedProxy,
  value: string | number | boolean,
) => {
  const next = Boolean(value)
  const key = `proxy:${client.id}:${proxy.name}`
  if (actionBusy.value) return
  actionBusy.value = key
  try {
    await setManagedProxyEnabled(client.id, proxy.name, next)
    ElMessage.success(
      next
        ? 'Proxy enabled; client is reconnecting'
        : 'Proxy disabled; client is reconnecting',
    )
    await refreshAll(true)
  } catch (error) {
    ElMessage.error('Failed to change proxy status: ' + errorMessage(error))
    await fetchClients(true)
  } finally {
    actionBusy.value = ''
  }
}

const copyText = async (text: string): Promise<boolean> => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // Fall back for HTTP dashboards and restricted browsers.
  }
  const activeElement = document.activeElement
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    textarea.remove()
    if (activeElement instanceof HTMLElement) activeElement.focus()
  }
}

const safeDownloadName = (name: string) => {
  const value = name.trim().replace(/[^a-zA-Z0-9._-]+/g, '_')
  return value || 'frpc'
}

const downloadText = (text: string, filename: string, contentType: string) => {
  const blob = new Blob([text], { type: contentType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

const showConfig = async (
  client: ManagedClient,
  action: 'view' | 'copy' | 'download' = 'view',
) => {
  if (configLoadingID.value) return
  configLoadingID.value = client.id
  try {
    const { toml } = await getManagedClientConfig(client.id)
    if (action === 'copy' && (await copyText(toml))) {
      ElMessage.success('frpc.toml copied')
      return
    }
    if (action === 'download') {
      downloadText(
        toml,
        `${safeDownloadName(client.name)}.toml`,
        'text/plain;charset=utf-8',
      )
      ElMessage.success('frpc.toml downloaded')
      return
    }
    configClientName.value = client.name
    configToml.value = toml
    configVisible.value = true
  } catch (error) {
    ElMessage.error('Failed to get client configuration: ' + errorMessage(error))
  } finally {
    configLoadingID.value = ''
  }
}

const copyPreview = async () => {
  if (await copyText(configToml.value)) {
    ElMessage.success('frpc.toml copied')
  } else {
    await nextTick()
    configTextarea.value?.focus()
    configTextarea.value?.select()
    ElMessage.info('Press Ctrl+C or use the browser Copy action.')
  }
}

const downloadPreview = () => {
  downloadText(
    configToml.value,
    `${safeDownloadName(configClientName.value)}.toml`,
    'text/plain;charset=utf-8',
  )
}

const rotateToken = async (client: ManagedClient) => {
  try {
    await ElMessageBox.confirm(
      `Reset the key for "${client.name}"? The current frpc connection will be disconnected and the old key will stop working immediately.`,
      'Reset client key',
      {
        confirmButtonText: 'Reset key',
        cancelButtonText: 'Cancel',
        type: 'warning',
      },
    )
    const result = await rotateManagedClientToken(client.id)
    configClientName.value = client.name
    configToml.value = result.toml
    configVisible.value = true
    ElMessage.success('Key reset. Deploy the newly generated frpc.toml.')
    await refreshAll(true)
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error('Failed to reset key: ' + errorMessage(error))
  }
}

const removeClient = async (client: ManagedClient) => {
  try {
    await ElMessageBox.confirm(
      `Delete "${client.name}" and release all assigned ports? The active client will be disconnected and this cannot be undone without a backup.`,
      'Delete managed client',
      {
        confirmButtonText: 'Delete',
        cancelButtonText: 'Cancel',
        type: 'warning',
      },
    )
    await deleteManagedClient(client.id)
    ElMessage.success('Client deleted')
    await refreshAll()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error('Failed to delete client: ' + errorMessage(error))
  }
}

const openPortPool = async () => {
  portPoolVisible.value = true
  await fetchPortPool()
}

const downloadBackup = async () => {
  try {
    const backup = await getManagedBackup()
    const date = new Date().toISOString().slice(0, 10)
    downloadText(
      JSON.stringify(backup, null, 2) + '\n',
      `frps-managed-clients-${date}.json`,
      'application/json;charset=utf-8',
    )
    ElMessage.success('Client archive backup downloaded')
  } catch (error) {
    ElMessage.error('Failed to create backup: ' + errorMessage(error))
  }
}

const triggerRestore = () => {
  restoreInput.value?.click()
}

const restoreBackupFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    const parsed = JSON.parse(await file.text()) as ManagedBackup
    if (!parsed || parsed.version !== 1 || !Array.isArray(parsed.clients)) {
      throw new Error('Unsupported managed-client backup format')
    }
    await ElMessageBox.confirm(
      `Restore ${parsed.clients.length} clients from this backup? Current managed clients and port assignments will be replaced, and active clients will be disconnected.`,
      'Restore client archive',
      {
        confirmButtonText: 'Restore',
        cancelButtonText: 'Cancel',
        type: 'warning',
      },
    )
    await restoreManagedBackup(parsed)
    ElMessage.success('Client archive restored')
    await refreshAll()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error('Failed to restore backup: ' + errorMessage(error))
  } finally {
    input.value = ''
  }
}

const eventTagType = (level: string): 'danger' | 'warning' | 'info' => {
  if (level === 'error') return 'danger'
  if (level === 'warning') return 'warning'
  return 'info'
}

const formatEventTime = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

onMounted(async () => {
  await refreshAll()
  refreshTimer = window.setInterval(async () => {
    await fetchClients(true)
    if (enabled.value) await fetchEvents(true)
  }, 5000)
})

onUnmounted(() => {
  requestSeq++
  window.clearInterval(refreshTimer)
})
</script>

<style scoped>
.managed-clients,
.managed-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.managed-content {
  min-height: 72px;
}

.section-header,
.managed-card-header,
.client-heading,
.client-actions,
.header-actions,
.proxy,
.proxy-editor-header,
.proxy-editor-title {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.section-header,
.managed-card-header,
.proxy-editor-header {
  justify-content: space-between;
}

.section-header.compact {
  align-items: flex-start;
}

h2,
h3,
p {
  margin: 0;
}

h2 {
  font-size: 18px;
  font-weight: 600;
}

h3 {
  font-size: 15px;
  font-weight: 600;
}

.section-header p,
.server-address,
.proxy-count,
.config-help,
.port-note,
.empty-proxies {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.section-header p {
  margin-top: 4px;
}

.managed-card,
.events-card {
  padding: 20px 24px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 16px;
  background: var(--el-bg-color);
}

.managed-card.disabled,
.proxy.disabled {
  opacity: 0.72;
}

.client-actions .el-button + .el-button {
  margin-left: 0;
}

.switch-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.server-address {
  margin-top: 8px;
  overflow-wrap: anywhere;
}

.client-id {
  margin-left: 16px;
  font-family: monospace;
  font-size: 11px;
}

.client-ports {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  margin-top: 10px;
}

.client-ports-label {
  flex: none;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.client-port-strip {
  display: flex;
  gap: 6px;
  min-width: 0;
  max-width: min(720px, 70vw);
  overflow: hidden;
  white-space: nowrap;
  cursor: default;
}

.port-chip {
  flex: none;
}

.port-tooltip {
  display: flex;
  flex-direction: column;
  gap: 7px;
  min-width: 320px;
  max-width: 560px;
}

.port-tooltip-row {
  display: grid;
  grid-template-columns: 10px auto minmax(80px, 1fr) auto;
  gap: 8px;
  align-items: center;
  font-size: 12px;
}

.port-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
}

.port-status-dot.online {
  background: var(--el-color-success);
}

.port-status-dot.offline {
  background: var(--el-color-danger);
}

.proxy-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.proxy {
  font-size: 13px;
  min-height: 32px;
}

.proxy-name {
  font-weight: 500;
  overflow-wrap: anywhere;
}

.proxy-route {
  color: var(--el-text-color-secondary);
  overflow-wrap: anywhere;
}

.proxy-spacer {
  flex: 1;
}

.empty-proxies {
  margin-top: 16px;
}

.events-card {
  margin-top: 8px;
}

.event-list {
  display: flex;
  flex-direction: column;
  margin-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.event-row {
  display: grid;
  grid-template-columns: 170px 150px minmax(130px, 220px) 1fr;
  gap: 12px;
  align-items: center;
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
}

.event-time,
.event-subject {
  color: var(--el-text-color-secondary);
}

.event-message {
  overflow-wrap: anywhere;
}

.hidden-file-input {
  display: none;
}

.editor-note {
  margin-bottom: 16px;
}

.client-fields,
.proxy-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.proxy-fields {
  grid-template-columns: repeat(6, minmax(0, 1fr));
}

.proxy-fields .el-form-item {
  grid-column: span 2;
  margin-bottom: 12px;
}

.proxy-fields .el-form-item:nth-child(-n + 2) {
  grid-column: span 3;
}

.proxy-fields .el-input-number,
.proxy-fields .el-select {
  width: 100%;
}

.proxies-heading {
  margin: 20px 0 12px;
}

.proxy-editor {
  padding: 16px;
  margin-bottom: 16px;
  border: 1px solid var(--el-border-color);
  border-radius: 12px;
}

.proxy-editor-header {
  margin-bottom: 12px;
}

.port-field {
  display: flex;
  gap: 8px;
  width: 100%;
}

.port-field .el-input-number {
  flex: 1;
}

.config-help {
  margin-bottom: 12px;
}

.config-preview {
  box-sizing: border-box;
  width: 100%;
  height: min(55vh, 480px);
  padding: 16px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-primary);
  font-family: monospace;
  font-size: 13px;
  line-height: 1.6;
  resize: vertical;
}

.config-preview:focus {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 2px;
}

.enable-config {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 8px 0 0;
  line-height: 1.6;
}

.port-loading {
  min-height: 160px;
}

.port-summary {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr;
  gap: 12px;
}

.port-summary > div {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px;
  border-radius: 10px;
  background: var(--el-fill-color-light);
}

.port-summary span {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.port-summary strong {
  font-size: 15px;
  overflow-wrap: anywhere;
}

.port-note {
  margin: 14px 0;
}

.table-scroll {
  overflow-x: auto;
}

.port-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.port-table th,
.port-table td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  text-align: left;
}

.port-table th {
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.empty-cell {
  color: var(--el-text-color-secondary);
  text-align: center !important;
}

@media (max-width: 900px) {
  .event-row {
    grid-template-columns: 140px 130px 1fr;
  }

  .event-message {
    grid-column: 1 / -1;
  }

  .port-summary {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .managed-card,
  .events-card {
    padding: 18px;
  }

  .client-fields,
  .proxy-fields {
    grid-template-columns: 1fr;
  }

  .proxy-fields .el-form-item,
  .proxy-fields .el-form-item:nth-child(-n + 2) {
    grid-column: auto;
  }

  .event-row {
    grid-template-columns: 1fr;
  }

  .event-message {
    grid-column: auto;
  }

  .header-actions,
  .client-actions {
    gap: 8px;
  }
}
</style>
