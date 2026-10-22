import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  cancelRefresh,
  cancelYuketangLogin,
  loadAssignments,
  loadBootstrap,
  loadSettings,
  refreshYuketangLogin,
  startRefresh,
  startYuketangLogin,
  toErrorText,
} from '../api/app'
import type {
  AccountStateEvent,
  AssignmentView,
  BootstrapData,
  QRCodeEvent,
  QRStateEvent,
  RefreshAllFinishedEvent,
  RefreshFinishedEvent,
  RefreshProgressEvent,
  SettingsView,
} from '../types'

interface LoginState {
  show: boolean
  jobId: string
  qrContent: string
  expiresAt: string
  state: string
  message: string
}

export const useAppStore = defineStore('app', () => {
  const bootstrap = ref<BootstrapData | null>(null)
  const assignments = ref<AssignmentView[]>([])
  const settings = ref<SettingsView | null>(null)

  const loading = ref(true)
  const listLoading = ref(false)
  const error = ref('')

  const showSubmitted = ref(false)
  const keyword = ref('')

  const refreshing = ref(false)
  const refreshJobId = ref('')
  const runningPlatforms = ref<string[]>([])
  const progressByPlatform = ref<Record<string, string>>({})
  const lastRefreshError = ref('')

  const login = reactive<LoginState>({
    show: false,
    jobId: '',
    qrContent: '',
    expiresAt: '',
    state: '',
    message: '',
  })

  let eventsBound = false

  function bindEvents() {
    if (eventsBound) return
    eventsBound = true

    EventsOn('refresh.progress', (ev: RefreshProgressEvent) => {
      const text = ev.message || (ev.total > 0 ? `${ev.current}/${ev.total}` : ev.phase)
      progressByPlatform.value = { ...progressByPlatform.value, [ev.platform]: text }
    })

    EventsOn('refresh.finished', (ev: RefreshFinishedEvent) => {
      const next = { ...progressByPlatform.value }
      delete next[ev.platform]
      progressByPlatform.value = next
      runningPlatforms.value = runningPlatforms.value.filter((p) => p !== ev.platform)
      if (ev.error) {
        lastRefreshError.value = ev.error.message
      }
    })

    EventsOn('refresh.all-finished', (_ev: RefreshAllFinishedEvent) => {
      refreshing.value = false
      refreshJobId.value = ''
      runningPlatforms.value = []
      progressByPlatform.value = {}
      void reloadAll()
    })

    EventsOn('auth.yuketang.qr', (ev: QRCodeEvent) => {
      login.qrContent = ev.qrContent
      login.expiresAt = ev.expiresAt
      login.state = 'waiting'
      login.message = '请使用雨课堂扫码登录'
    })

    EventsOn('auth.yuketang.state', (ev: QRStateEvent) => {
      login.state = ev.state
      login.message = ev.message
      if (ev.state === 'succeeded') {
        window.setTimeout(() => {
          login.show = false
          login.qrContent = ''
        }, 800)
        void reloadAll()
      }
    })

    EventsOn('account.state', (ev: AccountStateEvent) => {
      const data = bootstrap.value
      if (!data) return
      const item = data.platforms.find((p) => p.platform === ev.platform)
      if (item) item.loginState = ev.loginState
    })
  }

  async function init() {
    bindEvents()
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

  async function reloadAll() {
    try {
      bootstrap.value = await loadBootstrap()
    } catch (err) {
      error.value = toErrorText(err)
    }
    await refreshList()
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

  async function beginRefresh(platforms?: string[]) {
    lastRefreshError.value = ''
    const res = await startRefresh(platforms)
    if (res.jobId) {
      refreshJobId.value = res.jobId
      refreshing.value = true
      runningPlatforms.value = res.platforms ?? runningPlatforms.value
    }
    return res.alreadyRunning
  }

  async function stopRefresh() {
    if (!refreshJobId.value) return
    await cancelRefresh(refreshJobId.value)
  }

  async function beginLogin() {
    login.show = true
    login.qrContent = ''
    login.expiresAt = ''
    login.state = 'waiting'
    login.message = '正在获取二维码…'
    const res = await startYuketangLogin()
    login.jobId = res.jobId ?? ''
  }

  async function stopLogin() {
    const jobId = login.jobId
    login.show = false
    login.qrContent = ''
    login.state = ''
    login.message = ''
    login.jobId = ''
    if (jobId) {
      try {
        await cancelYuketangLogin(jobId)
      } catch {
        // 任务可能已自然结束，取消失败无需打扰用户
      }
    }
  }

  async function renewQR() {
    if (!login.jobId) return
    await refreshYuketangLogin(login.jobId)
    login.message = '正在获取新的二维码…'
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
    refreshing,
    refreshJobId,
    runningPlatforms,
    progressByPlatform,
    lastRefreshError,
    login,
    init,
    reloadAll,
    refreshList,
    setShowSubmitted,
    beginRefresh,
    stopRefresh,
    beginLogin,
    stopLogin,
    renewQR,
  }
})
