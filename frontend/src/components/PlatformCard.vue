<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NCard, NSpace, NTag } from 'naive-ui'
import type { PlatformView } from '../types'

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

const props = defineProps<{
  data: PlatformView
  refreshing?: boolean
  progress?: string
}>()

const emit = defineEmits<{
  (e: 'login'): void
  (e: 'refresh'): void
  (e: 'cancel'): void
  (e: 'issues'): void
}>()

const canLogin = computed(
  () =>
    props.data.platform === 'yuketang' ||
    props.data.platform === 'chaoxing' ||
    props.data.platform === 'tronclass' ||
    props.data.platform === 'pigai',
)
const loginText = computed(() => {
  const connected = props.data.loginState === 'connected'
  if (props.data.platform === 'tronclass') {
    return connected ? '重新导入' : '导入会话'
  }
  if (connected) return '重新登录'
  // 学习通扫码与账号密码都支持，按钮上不绑定具体方式
  return props.data.platform === 'chaoxing' ? '登录' : '扫码登录'
})

const platformNames: Record<string, string> = {
  chaoxing: '学习通',
  yuketang: '雨课堂',
  tronclass: '学在重邮',
  pigai: '批改网',
}

const loginMeta = computed<{ text: string; type: TagType }>(() => {
  switch (props.data.loginState) {
    case 'connected':
      return { text: '已登录', type: 'success' }
    case 'needs_login':
      return { text: '需要登录', type: 'warning' }
    case 'error':
      return { text: '登录异常', type: 'error' }
    default:
      return { text: '未登录', type: 'default' }
  }
})

const counts = computed(() => props.data.counts)

// 进度与错误共用同一行：这行的高度是固定的，卡片高度不随刷新的开始/结束而变化，
// 否则下方平台概览的滚动条会跟着上下跳
const hint = computed(() => (props.refreshing ? props.progress ?? '' : props.data.error?.message ?? ''))
const hintIsError = computed(() => !props.refreshing && Boolean(props.data.error))
</script>

<template>
  <n-card size="small" class="platform-card" :bordered="true">
    <template #header>
      <div class="platform-head">
        <span class="platform-name">{{ platformNames[data.platform] ?? data.platform }}</span>
        <n-tag size="small" :type="loginMeta.type" :bordered="false">
          {{ loginMeta.text }}
        </n-tag>
        <n-tag v-if="data.experimental" size="small" type="warning" :bordered="false">
          实验性
        </n-tag>
      </div>
    </template>

    <div class="counts">
      <div class="count-item">
        <strong>{{ counts.openUnsubmitted }}</strong>
        <span>待提交</span>
      </div>
      <div class="count-item danger">
        <strong>{{ counts.overdueUnsubmitted }}</strong>
        <span>已截止未交</span>
      </div>
      <div class="count-item">
        <strong>{{ counts.unknown }}</strong>
        <span>状态未知</span>
      </div>
      <div class="count-item muted">
        <strong>{{ counts.submitted }}</strong>
        <span>已提交</span>
      </div>
    </div>

    <div
      class="card-hint"
      :class="{ 'is-error': hintIsError }"
      :title="hintIsError ? data.error?.detail : hint"
    >
      {{ hint }}
    </div>

    <n-space class="card-actions" :size="8">
      <n-button v-if="canLogin" size="small" secondary @click="emit('login')">
        {{ loginText }}
      </n-button>
      <n-button
        v-if="!refreshing"
        size="small"
        type="primary"
        secondary
        @click="emit('refresh')"
      >
        刷新
      </n-button>
      <n-button v-else size="small" quaternary type="error" @click="emit('cancel')">
        取消刷新
      </n-button>
      <n-button
        v-if="data.lastRunId"
        size="small"
        quaternary
        @click="emit('issues')"
      >
        抓取明细
      </n-button>
    </n-space>
  </n-card>
</template>

<style scoped>
.platform-card {
  height: 100%;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

/* 边框色由 Naive UI 的 .n-card 规则控制，同权重下按注入顺序可能被盖掉，这里提高优先级 */
.platform-card:hover {
  transform: translateY(-4px);
  border-color: rgba(24, 160, 88, 0.55) !important;
  box-shadow: 0 10px 24px rgba(24, 160, 88, 0.14);
}

.platform-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.platform-name {
  font-size: 16px;
  font-weight: 600;
  margin-right: 4px;
}

.counts {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  text-align: center;
}

.count-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.count-item strong {
  font-size: 22px;
  line-height: 1.2;
}

.count-item span {
  font-size: 12px;
  opacity: 0.65;
}

.count-item.danger strong {
  color: #d03050;
}

.count-item.muted strong {
  opacity: 0.55;
}

/* 固定一行高度：刷新进度与错误提示共用，卡片高度因此不随状态跳变 */
.card-hint {
  margin-top: 10px;
  min-height: 18px;
  font-size: 12px;
  line-height: 18px;
  opacity: 0.65;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-hint.is-error {
  color: #d03050;
  opacity: 1;
}

.card-actions {
  margin-top: 12px;
}
</style>
