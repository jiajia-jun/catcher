<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NCard, NCollapse, NCollapseItem, NModal, NScrollbar, NTag } from 'naive-ui'
import { CHANGELOG } from '../changelog'

// 每个版本的固定收尾条目，避免手写时漏掉
const FIXED_NOTE = '修复了一些已知问题'

const props = defineProps<{ show: boolean; current: string }>()
const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'close'): void
}>()

// 默认只展开最新版本，历史版本折叠
const expandedNames = [CHANGELOG[0]?.version].filter((version): version is string => Boolean(version))

// 关闭时通知外层记录已读；更新日志按版本倒序排列，历史条目一并展示
const visible = computed({
  get: () => props.show,
  set: (value: boolean) => {
    emit('update:show', value)
    if (!value) emit('close')
  },
})
</script>

<template>
  <n-modal v-model:show="visible">
    <n-card
      style="width: 560px; max-width: 92vw"
      title="更新日志"
      :bordered="false"
      size="huge"
    >
      <n-scrollbar style="max-height: 58vh">
        <n-collapse :default-expanded-names="expandedNames">
          <n-collapse-item
            v-for="entry in CHANGELOG"
            :key="entry.version"
            :name="entry.version"
          >
            <template #header>
              <div class="entry-head">
                <strong>v{{ entry.version }}</strong>
                <n-tag
                  v-if="entry.version === current"
                  size="small"
                  type="success"
                  :bordered="false"
                >
                  当前版本
                </n-tag>
                <span class="entry-date">{{ entry.date }}</span>
              </div>
            </template>
            <ul class="entry-list">
              <li v-for="(item, index) in entry.items" :key="index">{{ item }}</li>
              <li>{{ FIXED_NOTE }}</li>
            </ul>
          </n-collapse-item>
        </n-collapse>
      </n-scrollbar>

      <template #footer>
        <div class="footer">
          <n-button type="primary" @click="visible = false">知道了</n-button>
        </div>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.entry-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.entry-date {
  margin-left: auto;
  font-size: 12px;
  opacity: 0.55;
}

.entry-list {
  margin: 0;
  padding-left: 18px;
  font-size: 13px;
  line-height: 1.75;
  opacity: 0.82;
}

.footer {
  display: flex;
  justify-content: flex-end;
}
</style>
