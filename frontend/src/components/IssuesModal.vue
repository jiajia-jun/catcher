<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NEmpty, NModal, NSpin, NTag, NText } from 'naive-ui'
import { loadFetchIssues, toErrorText } from '../api/app'
import type { FetchIssueView } from '../types'

const props = defineProps<{
  show: boolean
  runId: number
  platform: string
}>()

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
}>()

const items = ref<FetchIssueView[]>([])
const loading = ref(false)
const error = ref('')

const platformNames: Record<string, string> = {
  chaoxing: '学习通',
  yuketang: '雨课堂',
  tronclass: '学在重邮',
}

const title = computed(() => {
  const name = platformNames[props.platform] ?? props.platform
  return `${name}抓取明细`
})

function kindText(kind: string) {
  switch (kind) {
    case 'auth':
      return '登录态'
    case 'network':
      return '网络'
    case 'parse':
      return '页面结构'
    case 'business':
      return '平台返回'
    case 'canceled':
      return '已取消'
    default:
      return '内部错误'
  }
}

async function load() {
  if (!props.runId) return
  loading.value = true
  error.value = ''
  try {
    const res = await loadFetchIssues(props.runId)
    items.value = res.items ?? []
  } catch (err) {
    items.value = []
    error.value = toErrorText(err)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.show, props.runId] as const,
  ([show]) => {
    if (show) {
      void load()
    }
  },
  { immediate: true },
)

function onClose() {
  emit('update:show', false)
}
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="title"
    style="width: 640px"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <n-spin :show="loading">
      <n-text v-if="error" type="error">{{ error }}</n-text>
      <n-empty v-else-if="!loading && items.length === 0" description="这次抓取没有记录问题" />
      <div v-else class="issue-list">
        <div v-for="(item, index) in items" :key="index" class="issue-item">
          <div class="issue-head">
            <n-tag size="small" :bordered="false" type="warning">
              {{ kindText(item.kind) }}
            </n-tag>
            <span v-if="item.ref" class="issue-ref">{{ item.ref }}</span>
          </div>
          <div class="issue-message">{{ item.message }}</div>
          <div v-if="item.rawPath" class="issue-raw">留底：{{ item.rawPath }}</div>
        </div>
      </div>
    </n-spin>
  </n-modal>
</template>

<style scoped>
.issue-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 60vh;
  overflow: auto;
}

.issue-item {
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(128, 128, 128, 0.08);
}

.issue-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.issue-ref {
  font-size: 12px;
  opacity: 0.6;
}

.issue-message {
  margin-top: 6px;
  font-size: 13px;
  line-height: 1.6;
  word-break: break-word;
}

.issue-raw {
  margin-top: 6px;
  font-size: 12px;
  opacity: 0.6;
  word-break: break-all;
}
</style>
