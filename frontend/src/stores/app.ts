import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  cancelRefresh,
  cancelYuketangLogin,
  loadAssignments,
  loadBootstrap,
  loadSettings,
  loginChaoxing,
  refreshYuketangLogin,
  setAssignmentStatus,
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
  platform: '' | 'yuketang' | 'chaoxing'
  jobId: string
  qrContent: string
  expiresAt: string
  state: string
  message: string
  submitting: boolean
}

export const useAppStore = defineStore('app', () => {
  const bootstrap = ref<BootstrapData | null>(null)
  const assignments = ref<AssignmentView[]>([])
  const settings = ref<SettingsView | null>(null)

  const loading = ref(true)
  const listLoading = ref(false)
  const error = ref('')

  const showSubmitted = ref(false)
  const termOnly = ref(true)
  const keyword = ref('')

  const refreshing = ref(false)
  const refreshJobId = ref('')
  const runningPlatforms = ref<string[]>([])
  const progressByPlatform = ref<Record<string, string>>({})
  const lastRefreshError = ref('')

  const login = reactive<LoginState>({
    show: false,
    platform: '',
    jobId: '',
    qrContent: '',
    expiresAt: '',
    state: '',
    message: '',
    submitting: false,
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
      if (login.platform !== 'yuketang') return
      // 旧任务取消时可能晚到，已知当前 job 后只接收它的事件
      if (login.jobId && ev.jobId !== login.jobId) return
      login.qrContent = ev.qrContent
      login.expiresAt = ev.expiresAt
      login.state = 'waiting'
      login.message = '请使用雨课堂扫码登录'
    })

    EventsOn('auth.yuketang.state', (ev: QRStateEvent) => {
      if (login.platform !== 'yuketang') return
      if (login.jobId && ev.jobId !== login.jobId) return
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
        loadBootstrap(termOnly.value),
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
      bootstrap.value = await loadBootstrap(termOnly.value)
    } catch (err) {
      error.value = toErrorText(err)
    }
    await refreshList()
  }

  async function loadList(silent: boolean) {
    if (!silent) listLoading.value = true
    error.value = ''
    try {
      const res = await loadAssignments({
        showSubmitted: showSubmitted.value,
        keyword: keyword.value.trim(),
        termOnly: termOnly.value,
      })
      assignments.value = res.items
    } catch (err) {
      assignments.value = []
      error.value = toErrorText(err)
    } finally {
      if (!silent) listLoading.value = false
    }
  }

  async function refreshList() {
    await loadList(false)
  }

  // 手动改状态后静默同步列表与统计，不弹整页 loading
  async function reloadQuiet() {
    try {
      bootstrap.value = await loadBootstrap(termOnly.value)
    } catch (err) {
      error.value = toErrorText(err)
    }
    await loadList(true)
  }

  async function setStatus(id: number, status: 'submitted' | 'unsubmitted' | '') {
    const item = await setAssignmentStatus(id, status)
    await reloadQuiet()
    return item
  }

  async function setShowSubmitted(value: boolean) {
    showSubmitted.value = value
    await refreshList()
  }

  // 学期口径同时影响平台卡统计与列表，必须一起刷新
  async function setTermOnly(value: boolean) {
    termOnly.value = value
    await reloadAll()
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

  async function beginLogin(platform: 'yuketang' | 'chaoxing') {
    Object.assign(login, {
      show: true,
      platform,
      jobId: '',
      qrContent: '',
      expiresAt: '',
      state: 'waiting',
      message: '',
      submitting: false,
    })
    if (platform === 'yuketang') {
      login.message = '正在获取二维码…'
      const res = await startYuketangLogin()
      login.jobId = res.jobId ?? ''
    }
  }

  // 学习通使用手机号+密码登录，失败原因直接回显在弹窗里
  async function submitPasswordLogin(phone: string, password: string, remember: boolean) {
    login.submitting = true
    login.state = 'waiting'
    login.message = ''
    try {
      const res = await loginChaoxing({ phone, password, remember })
      if (!res.ok) {
        login.state = 'failed'
        login.message = res.error?.detail
          ? `${res.error.message}：${res.error.detail}`
          : res.error?.message ?? '登录失败'
        return false
      }
      login.state = 'succeeded'
      login.message = res.displayName ? `登录成功：${res.displayName}` : '登录成功'
      window.setTimeout(() => {
        if (login.platform === 'chaoxing') {
          login.show = false
          login.message = ''
          login.state = ''
        }
      }, 800)
      void reloadAll()
      return true
    } finally {
      login.submitting = false
    }
  }

  async function stopLogin() {
    const jobId = login.jobId
    login.show = false
    login.platform = ''
    login.qrContent = ''
    login.state = ''
    login.message = ''
    login.jobId = ''
    login.submitting = false
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
    termOnly,
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
    setStatus,
    setShowSubmitted,
    setTermOnly,
    beginRefresh,
    stopRefresh,
    beginLogin,
    submitPasswordLogin,
    stopLogin,
    renewQR,
  }
})
