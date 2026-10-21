<script setup lang="ts">
import { computed } from 'vue'
import { NCard, NTag } from 'naive-ui'
import type { PlatformView } from '../types'

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

const props = defineProps<{ data: PlatformView }>()

const platformNames: Record<string, string> = {
  chaoxing: '学习通',
  yuketang: '雨课堂',
  tronclass: '学在重邮',
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
  </n-card>
</template>

<style scoped>
.platform-card {
  height: 100%;
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
</style>
