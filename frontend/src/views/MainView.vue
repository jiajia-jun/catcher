<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  NAlert,
  NButton,
  NDataTable,
  NEmpty,
  NInput,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import IssuesModal from '../components/IssuesModal.vue'
import LoginModal from '../components/LoginModal.vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import PlatformCard from '../components/PlatformCard.vue'
import SettingsModal from '../components/SettingsModal.vue'
import { useAppStore } from '../stores/app'
import type { AssignmentView, PlatformView, RefreshFinishedEvent } from '../types'

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

const store = useAppStore()
const message = useMessage()

// 成功提示可以同时存在多条：新提示从底部进入，旧的被顺势顶上去，各自 2 秒后淡出
interface ToastItem {
  id: number
  title: string
  content: string
}

const toasts = ref<ToastItem[]>([])
const toastTimers = new Set<number>()
let toastSeq = 0
let offRefreshFinished: (() => void) | undefined

// 单条停留时长，到点后淡出并从列表移除
const toastDuration = 2000

function enqueueToast(item: { title: string; content: string }) {
  const id = ++toastSeq
  toasts.value.push({ id, ...item })
  const timer = window.setTimeout(() => {
    toastTimers.delete(timer)
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }, toastDuration)
  toastTimers.add(timer)
}

function showSuccessToast(ev: RefreshFinishedEvent) {
  if (ev.status !== 'success') return
  const name = platformNames[ev.platform] ?? ev.platform
  enqueueToast({
    title: `${name}抓取成功`,
    content: ev.counts.itemsUpserted > 0
      ? `课程 ${ev.counts.coursesOk}/${ev.counts.coursesTotal} 门，更新 ${ev.counts.itemsUpserted} 条作业`
      : `课程 ${ev.counts.coursesOk}/${ev.counts.coursesTotal} 门，当前学期没有作业`,
  })
}

const showSettings = ref(false)

const platforms = computed(() => store.bootstrap?.platforms ?? [])
const stats = computed(() => store.bootstrap?.stats ?? {
  openUnsubmitted: 0,
  overdueUnsubmitted: 0,
  unknown: 0,
  submitted: 0,
})

const platformNames: Record<string, string> = {
  chaoxing: '学习通',
  yuketang: '雨课堂',
  tronclass: '学在重邮',
}

const deadlineSourceNames: Record<string, string> = {
  platform_exact: '平台给出',
  detail_exact: '详情页',
  derived_remaining: '剩余时间推算',
  inferred_year: '年份推断',
  unknown: '未知',
}

function statusMeta(row: AssignmentView): { text: string; type: TagType } {
  if (row.status === 'submitted') return { text: '已提交', type: 'success' }
  if (row.status === 'unknown') return { text: '平台未提供', type: 'default' }
  if (row.phase === 'closed') return { text: '已截止未交', type: 'error' }
  return { text: '未提交', type: 'warning' }
}

const columns: DataTableColumns<AssignmentView> = [
  {
    title: '平台',
    key: 'platform',
    width: 104,
    render: (row) =>
      h(NTag, { size: 'small', bordered: false }, {
        default: () => platformNames[row.platform] ?? row.platform,
      }),
  },
  {
    title: '课程',
    key: 'courseName',
    width: 176,
    ellipsis: { tooltip: true },
    render: (row) => row.courseName || '未知课程',
  },
  {
    title: '作业',
    key: 'title',
    minWidth: 260,
    ellipsis: { tooltip: true },
  },
  {
    title: '状态',
    key: 'status',
    width: 132,
    render: (row) => {
      const meta = statusMeta(row)
      return h(NTag, { size: 'small', type: meta.type, bordered: false }, {
        default: () => meta.text,
      })
    },
  },
  {
    title: '截止时间',
    key: 'deadlineLocal',
    width: 168,
    render: (row) => {
      if (!row.deadlineLocal) return h(NText, { depth: 3 }, { default: () => '未知' })
      return row.deadlineLocal
    },
  },
  {
    title: '时间来源',
    key: 'deadlineSource',
    width: 128,
    render: (row) =>
      h(NText, { depth: 3 }, {
        default: () => deadlineSourceNames[row.deadlineSource] ?? row.deadlineSource,
      }),
  },
]

onMounted(() => {
  void store.init()
  // 直接用 Wails 事件驱动右下角提示，不依赖通知组件
  offRefreshFinished = EventsOn('refresh.finished', showSuccessToast)
})

onBeforeUnmount(() => {
  offRefreshFinished?.()
  for (const timer of toastTimers) window.clearTimeout(timer)
  toastTimers.clear()
})

function reportError(err: unknown) {
  message.error(err instanceof Error ? err.message : String(err))
}

async function onRefreshAll() {
  try {
    const running = await store.beginRefresh()
    if (running) message.info('所有可刷新平台都已在刷新中')
  } catch (err) {
    reportError(err)
  }
}

async function onRefreshPlatform(platform: string) {
  try {
    const running = await store.beginRefresh([platform])
    if (running) message.info('该平台已在刷新中')
  } catch (err) {
    reportError(err)
  }
}

async function onCancelRefresh() {
  try {
    await store.stopRefresh()
  } catch (err) {
    reportError(err)
  }
}

async function onLogin(platform: string) {
  if (platform !== 'yuketang' && platform !== 'chaoxing') return
  try {
    await store.beginLogin(platform)
  } catch (err) {
    reportError(err)
  }
}

const issues = ref({ show: false, runId: 0, platform: '' })

function onOpenIssues(item: PlatformView) {
  issues.value = { show: true, runId: item.lastRunId ?? 0, platform: item.platform }
}

function onToggleSubmitted(value: boolean) {
  void store.setShowSubmitted(value)
}

function onToggleTermOnly(value: boolean) {
  void store.setTermOnly(value)
}

const emptyText = computed(() => {
  const items = platforms.value
  const connected = items.some((p) => p.loginState === 'connected')
  if (!connected) return '还没有登录任何平台，先完成雨课堂扫码或学习通账号登录'
  if (store.refreshing) return '正在抓取作业，请稍候'
  if (store.keyword.trim()) return '没有匹配当前搜索的作业'
  return '当前筛选下没有作业，可以点「刷新全部」重新抓取'
})

function rowKey(row: AssignmentView) {
  return row.id
}
</script>

<template>
  <n-layout class="app-layout">
    <n-layout-header bordered class="topbar">
      <div class="brand">
        <span class="brand-name">Catcher</span>
        <span class="brand-sub">catch your work</span>
      </div>
      <div class="top-actions">
        <n-button quaternary @click="showSettings = true">设置</n-button>
      </div>
    </n-layout-header>

    <n-layout-content class="content">
      <n-spin :show="store.loading">
        <section class="section-head">
          <div>
            <h2>平台概览</h2>
            <p class="muted">
              待提交 {{ stats.openUnsubmitted }} · 已截止未交 {{ stats.overdueUnsubmitted }} ·
              状态未知 {{ stats.unknown }}
            </p>
          </div>
          <n-text v-if="store.bootstrap" depth="3">
            版本 {{ store.bootstrap.version }}
          </n-text>
        </section>

        <div class="platform-grid">
          <platform-card
            v-for="item in platforms"
            :key="item.platform"
            :data="item"
            :refreshing="store.runningPlatforms.includes(item.platform)"
            :progress="store.progressByPlatform[item.platform]"
            @login="onLogin(item.platform)"
            @refresh="onRefreshPlatform(item.platform)"
            @cancel="onCancelRefresh"
            @issues="onOpenIssues(item)"
          />
        </div>

        <section class="toolbar">
          <n-space align="center" :wrap="true">
            <n-input
              v-model:value="store.keyword"
              clearable
              placeholder="搜索作业或课程"
              style="width: 260px"
              @keyup.enter="store.refreshList"
              @clear="store.refreshList"
            />
            <n-button @click="store.refreshList">筛选</n-button>
            <span class="toggle-label" title="学期按 9 月 1 日划线；截止时间未知的作业仍会显示">
              只看本学期
            </span>
            <n-switch :value="store.termOnly" @update:value="onToggleTermOnly" />
            <span class="toggle-label">显示已提交</span>
            <n-switch :value="store.showSubmitted" @update:value="onToggleSubmitted" />
          </n-space>
          <n-space :size="8">
            <n-button :loading="store.listLoading" @click="store.refreshList">
              重新读取
            </n-button>
            <n-button
              v-if="!store.refreshing"
              type="primary"
              @click="onRefreshAll"
            >
              刷新全部
            </n-button>
            <n-button v-else type="error" secondary @click="onCancelRefresh">
              取消刷新
            </n-button>
          </n-space>
        </section>

        <n-alert
          v-if="store.error"
          type="error"
          title="读取失败"
          closable
          class="error-alert"
          @close="store.error = ''"
        >
          {{ store.error }}
        </n-alert>

        <n-alert
          v-if="store.lastRefreshError"
          type="warning"
          title="最近一次抓取未完全成功"
          closable
          class="error-alert"
          @close="store.lastRefreshError = ''"
        >
          {{ store.lastRefreshError }}
        </n-alert>

        <div v-else class="table-wrap">
          <n-data-table
            flex-height
            style="height: 100%"
            :columns="columns"
            :data="store.assignments"
            :loading="store.listLoading"
            :row-key="rowKey"
            :bordered="false"
            :single-line="false"
            size="small"
          >
            <template #empty>
              <n-empty :description="emptyText">
                <template #extra>
                  <div class="empty-hint">
                    作业数据只来自平台抓取，不会凭空生成；未登录或抓取失败时保留上次成功的数据。
                  </div>
                </template>
              </n-empty>
            </template>
          </n-data-table>
        </div>

        <p class="footer-note">
          所有数据只保存在本机数据目录；凭据使用 Windows DPAPI 加密。
        </p>
      </n-spin>
    </n-layout-content>

    <settings-modal v-model:show="showSettings" />
    <login-modal />
    <issues-modal
      v-model:show="issues.show"
      :run-id="issues.runId"
      :platform="issues.platform"
    />

    <transition-group name="toast" tag="div" class="toast-stack">
      <div v-for="item in toasts" :key="item.id" class="success-toast" role="status">
        <div class="success-toast-title">{{ item.title }}</div>
        <div class="success-toast-content">{{ item.content }}</div>
      </div>
    </transition-group>
  </n-layout>
</template>

<style scoped>
.app-layout {
  height: 100%;
}

.topbar {
  height: 64px;
  padding: 0 28px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.brand {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.brand-name {
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.2px;
}

.brand-sub {
  font-size: 12px;
  opacity: 0.55;
}

.top-actions {
  display: flex;
  align-items: center;
  gap: 16px;
  min-width: 0;
}

.content {
  padding: 24px 28px 32px;
  height: calc(100% - 64px);
  overflow: auto;
}

/* 让内容区变成纵向 flex，表格占满剩余高度 */
.content :deep(.n-spin-container),
.content :deep(.n-spin-content) {
  height: 100%;
}

.content :deep(.n-spin-content) {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

/* 行多时由表格自己滚动，表头固定 */
.table-wrap {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}

.section-head h2 {
  margin: 0 0 4px;
  font-size: 18px;
}

.muted {
  margin: 0;
  font-size: 13px;
  opacity: 0.62;
}

.platform-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 14px;
  margin-bottom: 22px;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.toggle-label {
  font-size: 13px;
  opacity: 0.72;
}

.error-alert {
  margin-bottom: 16px;
}

.empty-hint {
  max-width: 520px;
  margin-top: 8px;
  font-size: 13px;
  line-height: 1.6;
  opacity: 0.6;
}

.footer-note {
  margin: 22px 0 0;
  font-size: 12px;
  opacity: 0.5;
}

/* 多条提示从底部往上叠：容器固定在右下角，新条目追加在末尾 */
.toast-stack {
  position: fixed;
  right: 24px;
  bottom: 24px;
  z-index: 6000;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 10px;
  pointer-events: none;
}

.success-toast {
  min-width: 240px;
  max-width: 360px;
  padding: 12px 16px;
  border: 1px solid #18a058;
  border-radius: 10px;
  background: #e8f7ee;
  color: #18a058;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
}

.success-toast-title {
  font-size: 14px;
  font-weight: 600;
}

.success-toast-content {
  margin-top: 4px;
  font-size: 13px;
  opacity: 0.85;
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}

.toast-enter-from {
  opacity: 0;
  transform: translateY(16px);
}

.toast-leave-to {
  opacity: 0;
}

/* 新条目进入时，旧条目平滑上移 */
.toast-move {
  transition: transform 0.3s ease;
}
</style>
