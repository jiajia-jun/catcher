<script setup lang="ts">
import { computed } from 'vue'
import {
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NModal,
  NSpace,
  NTag,
  useMessage,
} from 'naive-ui'
import { openDataDir } from '../api/app'
import { useAppStore } from '../stores/app'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', value: boolean): void }>()

const store = useAppStore()
const message = useMessage()

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

const settings = computed(() => store.settings)

async function onOpenDir() {
  try {
    await openDataDir()
    message.success('已在资源管理器中打开数据目录')
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}
</script>

<template>
  <n-modal v-model:show="visible">
    <n-card
      style="width: 620px; max-width: 92vw"
      title="设置"
      :bordered="false"
      size="huge"
    >
      <n-descriptions v-if="settings" label-placement="left" :column="1" bordered>
        <n-descriptions-item label="版本">
          {{ settings.version }}
        </n-descriptions-item>
        <n-descriptions-item label="数据目录">
          <span class="path">{{ settings.dataDir }}</span>
        </n-descriptions-item>
        <n-descriptions-item label="日志级别">
          {{ settings.logLevel }}
        </n-descriptions-item>
        <n-descriptions-item label="失败响应留底">
          <n-tag size="small" :type="settings.rawEnabled ? 'success' : 'default'" :bordered="false">
            {{ settings.rawEnabled ? '开启' : '关闭' }}
          </n-tag>
          <span class="hint">保留 {{ settings.rawRetentionDays }} 天</span>
        </n-descriptions-item>
        <n-descriptions-item label="抓取历史">
          {{ settings.includeHistory ? '包含历史' : '仅当前学期' }}
        </n-descriptions-item>
        <n-descriptions-item label="主题">
          {{ settings.theme }}
        </n-descriptions-item>
      </n-descriptions>

      <template #footer>
        <n-space justify="space-between">
          <n-button @click="onOpenDir">打开数据目录</n-button>
          <n-button type="primary" @click="visible = false">关闭</n-button>
        </n-space>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.path {
  word-break: break-all;
  user-select: text;
}

.hint {
  margin-left: 8px;
  opacity: 0.6;
  font-size: 12px;
}
</style>
