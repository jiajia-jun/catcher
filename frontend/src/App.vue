<script setup lang="ts">
import { computed } from 'vue'
import { darkTheme, NConfigProvider, NMessageProvider, useOsTheme } from 'naive-ui'
import MainView from './views/MainView.vue'

const osTheme = useOsTheme()
const theme = computed(() => (osTheme.value === 'dark' ? darkTheme : null))
</script>

<template>
  <!-- 必须给高度，否则主布局的 height: 100% 会退化成内容高度，整页撑出窗口 -->
  <n-config-provider :theme="theme" style="height: 100%">
    <n-message-provider>
      <main-view />
    </n-message-provider>
  </n-config-provider>
</template>
