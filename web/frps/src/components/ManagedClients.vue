<template>
  <section class="managed-clients" aria-labelledby="managed-clients-title">
    <div class="section-header">
      <div>
        <h2 id="managed-clients-title">Managed clients</h2>
        <p>
          Create a client key, assign ports, and copy a ready-to-use frpc.toml.
        </p>
      </div>
      <el-button
        v-if="enabled"
        type="primary"
        :icon="Plus"
        @click="openCreateDialog"
      >
        Add client
      </el-button>
    </div>

    <el-alert v-if="loadError" type="error" :closable="false" show-icon>
      <template #title
        >Failed to load managed clients: {{ loadError }}</template
      >
      <el-button link type="primary" @click="fetchClients()">Retry</el-button>
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
          Set these options and restart frps to add clients here. Requires a
          dashboard username and password. Once enabled, only registered client
          keys can connect; the global token is no longer accepted.
        </p>
        <pre class="enable-config">
auth.method = "token"
clientManagement.enabled = true
clientManagement.storePath = "./frps-clients.json"</pre>
      </el-alert>
      <template v-else-if="enabled">
        <el-empty
          v-if="clients.length === 0 && !loading"
          description="No managed clients yet. Add a client to assign its ports."
          :image-size="72"
        />
        <article
          v-for="client in clients"
          :key="client.id"
          class="managed-card"
        >
          <div class="managed-card-header">
            <div class="client-heading">
              <h3>{{ client.name }}</h3>
              <el-tag :type="client.online ? 'success' : 'info'" size="small">
                {{ client.online ? 'Online' : 'Offline' }}
              </el-tag>
              <span class="proxy-count">
                {{ client.proxies.length }}
                {{ client.proxies.length === 1 ? 'proxy' : 'proxies' }}
              </span>
            </div>
            <div class="client-actions">
              <router-link
                v-if="client.key"
                :to="{ name: 'ClientDetail', params: { key: client.key } }"
              >
                <el-button link type="primary">Connection details</el-button>
              </router-link>
              <el-button
                :loading="configLoadingID === client.id"
                :disabled="configLoadingID !== ''"
                @click="showConfig(client)"
              >
                View TOML
              </el-button>
              <el-button
                type="primary"
                plain
                :icon="CopyDocument"
                :loading="configLoadingID === client.id"
                :disabled="configLoadingID !== ''"
                @click="showConfig(client, true)"
              >
                Copy frpc.toml
              </el-button>
            </div>
          </div>
          <p class="server-address">Server: {{ client.serverAddr }}</p>
          <div class="proxy-list">
            <div
              v-for="proxy in client.proxies"
              :key="proxy.name"
              class="proxy"
            >
              <el-tag size="small" type="info">{{
                proxy.type.toUpperCase()
              }}</el-tag>
              <span class="proxy-name">{{ proxy.name }}</span>
              <span class="proxy-route">
                Remote {{ proxy.remotePort }}
                <span aria-label="forwards to">→</span>
                {{ proxy.localIP }}:{{ proxy.localPort }}
              </span>
            </div>
          </div>
        </article>
      </template>
    </div>

    <el-dialog
      v-model="createVisible"
      title="Add client"
      width="min(820px, 94vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @closed="form.token = ''"
    >
      <el-form
        label-position="top"
        :disabled="saving"
        @submit.prevent="saveClient"
      >
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
            <span class="field-help"
              >The public IP or hostname used by frpc.</span
            >
          </el-form-item>
        </div>
        <el-form-item label="Client key (auth.token)">
          <el-input
            v-model="form.token"
            type="password"
            show-password
            autocomplete="new-password"
            placeholder="Leave blank to generate a secure key"
          />
          <span class="field-help"
            >Use a unique key for this client. It is included in the copied
            TOML.</span
          >
        </el-form-item>

        <div class="section-header proxies-heading">
          <div>
            <h3>Proxies</h3>
            <p>
              This key can only open the TCP/UDP remote ports assigned below.
            </p>
          </div>
          <el-button
            :icon="Plus"
            :disabled="form.proxies.length >= 100"
            @click="addProxy"
            >Add proxy</el-button
          >
        </div>
        <div
          v-for="(proxy, index) in form.proxies"
          :key="proxy.rowID"
          class="proxy-editor"
        >
          <div class="proxy-editor-header">
            <strong>Proxy {{ index + 1 }}</strong>
            <el-button
              type="danger"
              link
              :disabled="form.proxies.length === 1"
              :aria-label="`Remove proxy ${index + 1}`"
              @click="form.proxies.splice(index, 1)"
            >
              Remove
            </el-button>
          </div>
          <div class="proxy-fields">
            <el-form-item label="Name" required>
              <el-input v-model="proxy.name" placeholder="ssh" />
            </el-form-item>
            <el-form-item label="Type" required>
              <el-select v-model="proxy.type" aria-label="Proxy type">
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
              <el-input-number
                v-model="proxy.remotePort"
                :min="1"
                :max="65535"
                :precision="0"
                controls-position="right"
              />
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
        <el-button :disabled="saving" @click="createVisible = false"
          >Cancel</el-button
        >
        <el-button type="primary" :loading="saving" @click="saveClient"
          >Create client</el-button
        >
      </template>
    </el-dialog>

    <el-dialog
      v-model="configVisible"
      :title="`${configClientName} — frpc.toml`"
      width="min(760px, 94vw)"
      @closed="configToml = ''"
    >
      <p class="config-help">
        Save this as frpc.toml and run <code>frpc -c frpc.toml</code>. This file
        contains the client key.
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
        <el-button type="primary" :icon="CopyDocument" @click="copyPreview"
          >Copy frpc.toml</el-button
        >
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, Plus } from '@element-plus/icons-vue'
import {
  createManagedClient,
  getManagedClientConfig,
  getManagedClients,
} from '../api/client'
import type {
  ManagedClient,
  ManagedClientInput,
  ManagedProxy,
} from '../types/client'

interface ProxyDraft extends ManagedProxy {
  rowID: number
}

const clients = ref<ManagedClient[]>([])
const enabled = ref(false)
const loaded = ref(false)
const loading = ref(true)
const loadError = ref('')
const createVisible = ref(false)
const saving = ref(false)
const saveError = ref('')
const configLoadingID = ref('')
const configVisible = ref(false)
const configClientName = ref('')
const configToml = ref('')
const configTextarea = ref<HTMLTextAreaElement>()
const form = reactive({
  name: '',
  token: '',
  serverAddr: '',
  proxies: [] as ProxyDraft[],
})
let nextProxyID = 0
let refreshTimer: number | undefined
let requestSeq = 0

const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

const fetchClients = async (silent = false) => {
  const seq = ++requestSeq
  if (!silent) loading.value = true
  try {
    const data = await getManagedClients()
    if (seq !== requestSeq) return
    enabled.value = data.enabled
    clients.value = data.items
    loaded.value = true
    loadError.value = ''
  } catch (error) {
    if (seq === requestSeq) loadError.value = errorMessage(error)
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

const addProxy = () => {
  const rowID = ++nextProxyID
  const usedPorts = new Set(form.proxies.map((proxy) => proxy.remotePort))
  let remotePort = 6000
  while (usedPorts.has(remotePort)) remotePort++
  form.proxies.push({
    rowID,
    name: `proxy-${rowID}`,
    type: 'tcp',
    localIP: '127.0.0.1',
    localPort: 80,
    remotePort,
  })
}

const openCreateDialog = () => {
  form.name = ''
  form.token = ''
  form.serverAddr = window.location.hostname.replace(/^\[|\]$/g, '')
  form.proxies = []
  nextProxyID = 0
  addProxy()
  saveError.value = ''
  createVisible.value = true
}

const validPort = (value: number) =>
  Number.isInteger(value) && value >= 1 && value <= 65535

const saveClient = async () => {
  if (saving.value) return
  saveError.value = ''
  const input: ManagedClientInput = {
    name: form.name.trim(),
    token: form.token || undefined,
    serverAddr: form.serverAddr.trim(),
    proxies: form.proxies.map(
      ({ name, type, localIP, localPort, remotePort }) => ({
        name: name.trim(),
        type,
        localIP: localIP.trim(),
        localPort,
        remotePort,
      }),
    ),
  }
  if (!input.name || !input.serverAddr) {
    saveError.value = 'Enter a client name and server address.'
    return
  }
  if (input.serverAddr.includes('://') || /\s|\//.test(input.serverAddr)) {
    saveError.value =
      'Enter a server hostname or IP address without a URL scheme or path.'
    return
  }
  const names = new Set<string>()
  const ports = new Set<string>()
  for (const [index, proxy] of input.proxies.entries()) {
    if (
      !proxy.name ||
      !proxy.localIP ||
      !validPort(proxy.localPort) ||
      !validPort(proxy.remotePort)
    ) {
      saveError.value = `Proxy ${index + 1}: enter a name, local address, and ports between 1 and 65535.`
      return
    }
    if (names.has(proxy.name)) {
      saveError.value = `Proxy names must be unique: ${proxy.name}.`
      return
    }
    const portKey = `${proxy.type}:${proxy.remotePort}`
    if (ports.has(portKey)) {
      saveError.value = `Remote port ${proxy.remotePort} is assigned more than once for ${proxy.type.toUpperCase()}.`
      return
    }
    names.add(proxy.name)
    ports.add(portKey)
  }
  saving.value = true
  try {
    await createManagedClient(input)
    createVisible.value = false
    form.token = ''
    ElMessage.success('Client created. Copy its frpc.toml to connect.')
    await fetchClients()
  } catch (error) {
    saveError.value = errorMessage(error)
  } finally {
    saving.value = false
  }
}

const copyText = async (text: string): Promise<boolean> => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // HTTP dashboards and restricted browsers may require the selection fallback.
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

const showConfig = async (client: ManagedClient, copy = false) => {
  if (configLoadingID.value) return
  configLoadingID.value = client.id
  try {
    const { toml } = await getManagedClientConfig(client.id)
    if (copy && (await copyText(toml))) {
      ElMessage.success('frpc.toml copied')
      return
    }
    configClientName.value = client.name
    configToml.value = toml
    configVisible.value = true
    if (copy) ElMessage.info('Select and copy the configuration below.')
  } catch (error) {
    ElMessage.error(
      'Failed to get client configuration: ' + errorMessage(error),
    )
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
    ElMessage.info(
      'Press Ctrl+C or use your browser’s Copy action to copy the selected text.',
    )
  }
}

onMounted(() => {
  fetchClients()
  refreshTimer = window.setInterval(() => fetchClients(true), 5000)
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
.proxy,
.proxy-editor-header {
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
.field-help,
.config-help {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.section-header p {
  margin-top: 4px;
}

.managed-card {
  padding: 20px 24px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 16px;
  background: var(--el-bg-color);
}

.client-actions .el-button + .el-button {
  margin-left: 0;
}

.server-address {
  margin-top: 8px;
  overflow-wrap: anywhere;
}

.proxy-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
}

.proxy {
  font-size: 13px;
}

.proxy-name {
  font-weight: 500;
  overflow-wrap: anywhere;
}

.proxy-route {
  color: var(--el-text-color-secondary);
  overflow-wrap: anywhere;
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

@media (max-width: 640px) {
  .managed-card {
    padding: 20px;
  }

  .client-fields,
  .proxy-fields {
    grid-template-columns: 1fr;
  }

  .proxy-fields .el-form-item,
  .proxy-fields .el-form-item:nth-child(-n + 2) {
    grid-column: auto;
  }

  .client-actions {
    gap: 8px;
  }
}
</style>
