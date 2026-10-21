import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  loadAssignments,
  loadBootstrap,
  loadSettings,
  toErrorText,
} from '../api/app'
import type {
  AssignmentView,
  BootstrapData,
  SettingsView,
} from '../types'

export const useAppStore = defineStore('app', () => {
  const bootstrap = ref<BootstrapData | null>(null)
  const assignments = ref<AssignmentView[]>([])
  const settings = ref<SettingsView | null>(null)

  const loading = ref(true)
  const listLoading = ref(false)
  const error = ref('')

  const showSubmitted = ref(false)
  const keyword = ref('')

  async function init() {
    loading.value = true
    error.value = ''
    try {
      const [bootstrapData, settingsData] = await Promise.all([
        loadBootstrap(),
        loadSettings(),
      ])
      bootstrap.value = bootstrapData
      settings.value = settingsData
      await refreshList()
    } catch (err) {
      error.value = toErrorText(err)
    } finally {
      loading.value = false
    }
  }

  async function refreshList() {
    listLoading.value = true
    error.value = ''
    try {
      const res = await loadAssignments({
        showSubmitted: showSubmitted.value,
        keyword: keyword.value.trim(),
      })
      assignments.value = res.items
    } catch (err) {
      assignments.value = []
      error.value = toErrorText(err)
    } finally {
      listLoading.value = false
    }
  }

  async function setShowSubmitted(value: boolean) {
    showSubmitted.value = value
    await refreshList()
  }

  return {
    bootstrap,
    assignments,
    settings,
    loading,
    listLoading,
    error,
    showSubmitted,
    keyword,
    init,
    refreshList,
    setShowSubmitted,
  }
})
