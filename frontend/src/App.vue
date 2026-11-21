<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import { darkTheme, NConfigProvider, NMessageProvider, useOsTheme } from 'naive-ui'
import MainView from './views/MainView.vue'
import { useAppStore } from './stores/app'
import { githubDarkOverrides } from './theme'

const store = useAppStore()
const osTheme = useOsTheme()

// 设置加载前按系统主题渲染：ui.theme 为 auto 时这就是最终结果
const isDark = computed(() => {
  const mode = store.settings?.theme
  if (mode === 'dark') return true
  if (mode === 'light') return false
  return osTheme.value === 'dark'
})

const theme = computed(() => (isDark.value ? darkTheme : null))
// 浅色继续用 Naive UI 默认主题，只有深色换成 GitHub 配色
const themeOverrides = computed(() => (isDark.value ? githubDarkOverrides : undefined))

// Naive UI 不往根元素写变量，自定义样式靠 data-theme 取对应的一组
watchEffect(() => {
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light'
})
</script>

<template>
  <!-- 必须给高度，否则主布局的 height: 100% 会退化成内容高度，整页撑出窗口 -->
  <n-config-provider :theme="theme" :theme-overrides="themeOverrides" style="height: 100%">
    <n-message-provider>
      <main-view />
    </n-message-provider>
  </n-config-provider>
</template>
