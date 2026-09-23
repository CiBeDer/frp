<template>
  <div class="traffic-chart-container" v-loading="loading">
    <div class="traffic-controls">
      <ElDatePicker
        v-model="selectedRange"
        type="daterange"
        value-format="YYYY-MM-DD"
        format="YYYY-MM-DD"
        start-placeholder="Start date"
        end-placeholder="End date"
        range-separator="to"
        :disabled="loading || history.length === 0"
        :disabled-date="isDateDisabled"
        :clearable="false"
        class="date-range"
      />
      <el-button
        type="primary"
        :disabled="loading || !selectedRange || history.length === 0"
        @click="applyRange"
        >Query</el-button
      >
    </div>
    <p class="traffic-note">
      Daily totals for the last 7 days, using server dates. Both selected dates
      are included. Total = In + Out.
    </p>

    <el-alert v-if="error" :title="error" type="error" :closable="false">
      <el-button size="small" @click="fetchData">Retry</el-button>
    </el-alert>

    <template v-if="!loading && !error && chartData.length > 0">
      <div class="range-summary">
        <div class="summary-item">
          <span>Selected total</span>
          <strong>{{
            formatFileSize(totals.trafficIn + totals.trafficOut)
          }}</strong>
        </div>
        <div class="summary-item">
          <span>Traffic In</span>
          <strong>{{ formatFileSize(totals.trafficIn) }}</strong>
        </div>
        <div class="summary-item">
          <span>Traffic Out</span>
          <strong>{{ formatFileSize(totals.trafficOut) }}</strong>
        </div>
      </div>
      <p v-if="appliedRange" class="applied-range">
        {{ appliedRange[0] }} to {{ appliedRange[1] }}
      </p>
    </template>
    <div v-if="!loading && chartData.length > 0" class="chart-wrapper">
      <div class="y-axis">
        <div class="y-label">{{ formatFileSize(maxVal) }}</div>
        <div class="y-label">{{ formatFileSize(maxVal / 2) }}</div>
        <div class="y-label">0</div>
      </div>

      <div class="bars-area">
        <!-- Grid Lines -->
        <div class="grid-line top"></div>
        <div class="grid-line middle"></div>
        <div class="grid-line bottom"></div>

        <div v-for="item in chartData" :key="item.date" class="day-column">
          <div class="bars-group">
            <el-tooltip
              :content="`${item.date} · In: ${formatFileSize(item.in)}`"
              placement="top"
            >
              <div
                class="bar bar-in"
                :style="{ height: Math.max(item.inPercent, 1) + '%' }"
              ></div>
            </el-tooltip>
            <el-tooltip
              :content="`${item.date} · Out: ${formatFileSize(item.out)}`"
              placement="top"
            >
              <div
                class="bar bar-out"
                :style="{ height: Math.max(item.outPercent, 1) + '%' }"
              ></div>
            </el-tooltip>
          </div>
          <div class="date-label" :title="item.date">
            <span>{{ formatDateLabel(item.date) }}</span>
            <strong :title="`Total: ${item.in + item.out} bytes`">{{
              formatFileSize(item.in + item.out)
            }}</strong>
          </div>
        </div>
      </div>
    </div>

    <!-- Legend -->
    <div v-if="!loading && chartData.length > 0" class="legend">
      <div class="legend-item"><span class="dot in"></span> Traffic In</div>
      <div class="legend-item"><span class="dot out"></span> Traffic Out</div>
    </div>

    <el-empty v-else-if="!loading && !error" description="No traffic data" />
    <p class="traffic-note">
      In-memory statistics reset when frps restarts. TCP traffic is counted when
      connections close.
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElDatePicker, ElMessage } from 'element-plus'
import { formatFileSize } from '../utils/format'
import { getProxyTraffic } from '../api/proxy'
import type { TrafficResponse } from '../types/proxy'

const props = defineProps<{
  proxyName: string
}>()

const loading = ref(false)
const error = ref('')
const history = ref<TrafficResponse['history']>([])
const selectedRange = ref<[string, string] | null>(null)
const appliedRange = ref<[string, string] | null>(null)
let requestSeq = 0

const points = computed(() => {
  const range = appliedRange.value
  if (!range) return []
  return history.value.filter(
    (item) => item.date >= range[0] && item.date <= range[1],
  )
})

const totals = computed(() =>
  points.value.reduce(
    (sum, item) => ({
      trafficIn: sum.trafficIn + item.trafficIn,
      trafficOut: sum.trafficOut + item.trafficOut,
    }),
    { trafficIn: 0, trafficOut: 0 },
  ),
)

const maxVal = computed(() =>
  Math.max(
    100,
    ...points.value.flatMap((item) => [item.trafficIn, item.trafficOut]),
  ),
)

const chartData = computed(() =>
  points.value.map((item) => ({
    date: item.date,
    in: item.trafficIn,
    out: item.trafficOut,
    inPercent: (item.trafficIn / maxVal.value) * 100,
    outPercent: (item.trafficOut / maxVal.value) * 100,
  })),
)

const availableDates = computed(
  () => new Set(history.value.map((item) => item.date)),
)

const isDateDisabled = (date: Date) => {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return !availableDates.value.has(`${date.getFullYear()}-${month}-${day}`)
}

const applyRange = () => {
  const range = selectedRange.value
  if (
    !range ||
    range[0] > range[1] ||
    !availableDates.value.has(range[0]) ||
    !availableDates.value.has(range[1])
  ) {
    ElMessage.warning('Choose dates within the available 7-day history.')
    return
  }
  appliedRange.value = [range[0], range[1]]
}

const formatDateLabel = (date: string) => {
  const parts = date.split('-')
  if (parts.length !== 3) return date
  return `${Number(parts[1])}-${Number(parts[2])}`
}

const fetchData = async () => {
  const seq = ++requestSeq
  loading.value = true
  error.value = ''
  history.value = []
  selectedRange.value = null
  appliedRange.value = null
  try {
    const json = await getProxyTraffic(props.proxyName)
    if (seq !== requestSeq) return
    history.value = [...(json.history || [])].sort((a, b) =>
      a.date.localeCompare(b.date),
    )
    const first = history.value[0]
    const last = history.value[history.value.length - 1]
    if (first && last) {
      selectedRange.value = [first.date, last.date]
      applyRange()
    }
  } catch (err) {
    if (seq === requestSeq) {
      error.value =
        'Failed to load traffic: ' +
        (err instanceof Error ? err.message : String(err))
    }
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

watch(() => props.proxyName, fetchData, { immediate: true })
</script>

<style scoped>
.traffic-chart-container {
  width: 100%;
  min-height: 400px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.traffic-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.date-range {
  max-width: 360px;
  min-width: 0;
}

.traffic-note,
.applied-range {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

.range-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 20px 40px;
  padding: 16px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
}

.summary-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.summary-item strong {
  font-size: 20px;
  color: var(--el-text-color-primary);
}

.chart-wrapper {
  height: 260px;
  flex-shrink: 0;
  display: flex;
  gap: 10px;
  position: relative;
  margin-bottom: 48px;
}

.y-axis {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  text-align: right;
  font-size: 12px;
  color: #909399;
  height: 100%;
}

.bars-area {
  flex: 1;
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  position: relative;
  height: 100%;
}

.grid-line {
  position: absolute;
  left: 0;
  right: 0;
  height: 1px;
  background-color: #e4e7ed;
  z-index: 0;
}

html.dark .grid-line {
  background-color: #3a3d5c;
}

.grid-line.top {
  top: 0;
}
.grid-line.middle {
  top: 50%;
  transform: translateY(-50%);
}
.grid-line.bottom {
  bottom: 0;
}

.day-column {
  flex: 1;
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  align-items: center;
  position: relative;
  z-index: 1;
}

.bars-group {
  height: 100%;
  display: flex;
  align-items: flex-end;
  gap: 4px;
  width: 60%;
}

.bar {
  flex: 1;
  border-radius: 4px 4px 0 0;
  transition: height 0.3s ease;
  min-height: 1px;
}

.bar-in {
  background-color: #5470c6;
}

.bar-out {
  background-color: #91cc75;
}

.bar:hover {
  opacity: 0.8;
}

.date-label {
  position: absolute;
  bottom: -44px;
  font-size: 12px;
  color: #909399;
  width: 100%;
  text-align: center;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.date-label strong {
  font-weight: 500;
  color: var(--el-text-color-primary);
  font-size: clamp(10px, 1.2vw, 12px);
}

.legend {
  display: flex;
  justify-content: center;
  flex-wrap: wrap;
  gap: 24px;
  margin-top: 10px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: #606266;
}

html.dark .legend-item {
  color: #e5e7eb;
}

.dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.dot.in {
  background-color: #5470c6;
}
.dot.out {
  background-color: #91cc75;
}
</style>
