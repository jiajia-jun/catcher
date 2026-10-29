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
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime'
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

const FEEDBACK_URL = 'https://github.com/jiajia-jun/catcher/issues'

async function onOpenDir() {
  try {
    await openDataDir()
    message.success('已在资源管理器中打开数据目录')
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

function onFeedback() {
  BrowserOpenURL(FEEDBACK_URL)
}

const updateInfo = computed(() => store.updateInfo)

// 手动检查无论成败都要给明确反馈；自动检查失败是静默的
function onCheckUpdate() {
  void store
    .checkUpdateManually()
    .then((res) => {
      if (!res) return
      if (res.hasUpdate) message.info(`发现新版本 ${res.latest}`)
      else message.success(`已是最新版本 ${res.current}`)
    })
    .catch((err: unknown) => {
      message.error(err instanceof Error ? err.message : String(err))
    })
}

function onOpenRelease() {
  if (updateInfo.value.releaseUrl) BrowserOpenURL(updateInfo.value.releaseUrl)
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
          <span>{{ settings.version }}</span>
          <n-tag
            v-if="updateInfo.hasUpdate"
            size="small"
            type="info"
            :bordered="false"
            class="update-tag"
            @click="onOpenRelease"
          >
            新版本 {{ updateInfo.latest }}
          </n-tag>
          <n-button
            size="tiny"
            class="check-button"
            :loading="updateInfo.checking"
            :disabled="updateInfo.checking"
            @click="onCheckUpdate"
          >
            检查更新
          </n-button>
        </n-descriptions-item>
        <n-descriptions-item label="更新日志">
          <n-button size="tiny" @click="store.openChangelog()">查看更新日志</n-button>
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
        <n-descriptions-item label="反馈">
          <a href="#" class="feedback-link" @click.prevent="onFeedback">提交问题或建议</a>
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

.update-tag {
  margin-left: 10px;
  cursor: pointer;
}

.check-button {
  margin-left: 12px;
}

.feedback-link {
  color: #18a058;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.feedback-link:hover {
  color: #0f7b41;
}
</style>
