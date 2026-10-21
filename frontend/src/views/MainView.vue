<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
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
import PlatformCard from '../components/PlatformCard.vue'
import SettingsModal from '../components/SettingsModal.vue'
import { useAppStore } from '../stores/app'
import type { AssignmentView } from '../types'

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

const store = useAppStore()
const message = useMessage()

const showSettings = ref(false)

const platforms = computed(() => store.bootstrap?.platforms ?? [])
const dataDir = computed(() => store.bootstrap?.dataDir ?? '')
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
})

async function onRefresh() {
  await store.refreshList()
  if (store.error) {
    message.error(store.error)
    return
  }
  message.success('已重新读取本地数据；抓取适配器将在 M1 接入')
}

function onToggleSubmitted(value: boolean) {
  void store.setShowSubmitted(value)
}

function rowKey(row: AssignmentView) {
  return row.id
}
</script>

<template>
  <n-layout class="app-layout">
    <n-layout-header bordered class="topbar">
      <div class="brand">
        <span class="brand-name">Catcher</span>
        <span class="brand-sub">作业聚合 · 本地单用户</span>
      </div>
      <div class="top-actions">
        <n-text depth="3" class="data-dir" :title="dataDir">{{ dataDir }}</n-text>
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
          <platform-card v-for="item in platforms" :key="item.platform" :data="item" />
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
            <span class="toggle-label">显示已提交</span>
            <n-switch :value="store.showSubmitted" @update:value="onToggleSubmitted" />
          </n-space>
          <n-button type="primary" :loading="store.listLoading" @click="onRefresh">
            刷新本地数据
          </n-button>
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

        <n-data-table
          v-else
          :columns="columns"
          :data="store.assignments"
          :loading="store.listLoading"
          :row-key="rowKey"
          :bordered="false"
          :single-line="false"
          size="small"
        >
          <template #empty>
            <n-empty description="还没有作业数据">
              <template #extra>
                <div class="empty-hint">
                  M0 已完成基础设施；抓取适配器将在 M1（雨课堂）与 M2（学习通）接入。
                </div>
              </template>
            </n-empty>
          </template>
        </n-data-table>

        <p class="footer-note">
          所有数据只保存在本机数据目录；凭据使用 Windows DPAPI 加密。
        </p>
      </n-spin>
    </n-layout-content>

    <settings-modal v-model:show="showSettings" />
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

.data-dir {
  max-width: 460px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.content {
  padding: 24px 28px 32px;
  height: calc(100% - 64px);
  overflow: auto;
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
</style>
