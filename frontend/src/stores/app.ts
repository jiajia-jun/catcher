import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  cancelQRLogin,
  cancelRefresh,
  checkForUpdate,
  loadAssignments,
  loadBootstrap,
  loadSettings,
  loginChaoxing,
  importTronclassSession,
  markChangelogSeen,
  refreshQRLogin,
  saveEnabledPlatforms,
  setAssignmentStatus,
  startQRLogin,
  startRefresh,
  toErrorText,
} from '../api/app'
import type { QRPlatform } from '../api/app'
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
  UpdateCheckResult,
} from '../types'

// 扫码登录的平台集合。三个平台的事件结构一致，处理逻辑共用一份
const QR_PLATFORMS = ['yuketang', 'chaoxing', 'pigai'] as const

// 平台展示顺序与后端 entity.AllPlatforms 保持一致
const PLATFORM_ORDER = ['chaoxing', 'yuketang', 'tronclass', 'pigai']

type LoginPlatform = '' | 'yuketang' | 'chaoxing' | 'tronclass' | 'pigai'

function isQRPlatform(platform: LoginPlatform): platform is QRPlatform {
  return (QR_PLATFORMS as readonly string[]).includes(platform)
}

// 取码成功后展示的扫码提示，按平台给出对应的 App 名字
function qrHint(platform: QRPlatform): string {
  switch (platform) {
    case 'chaoxing':
      return '请使用学习通 App 扫码登录'
    case 'pigai':
      return '请使用微信扫一扫登录批改网'
    default:
      return '请使用雨课堂扫码登录'
  }
}

interface LoginState {
  show: boolean
  platform: LoginPlatform
  // 学习通支持扫码与账号密码两种方式，其余平台忽略
  mode: 'qr' | 'password'
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
  // 点击平台卡后只显示该平台的作业，空串表示不筛选
  const selectedPlatform = ref('')
  // 「刷新全部」勾选的平台，至少一个；由后端归一后落盘
  const enabledPlatforms = ref<string[]>([...PLATFORM_ORDER])
  const changelogShow = ref(false)

  const refreshing = ref(false)
  const refreshJobId = ref('')
  const runningPlatforms = ref<string[]>([])
  const progressByPlatform = ref<Record<string, string>>({})
  const lastRefreshError = ref('')

  const login = reactive<LoginState>({
    show: false,
    platform: '',
    mode: 'qr',
    jobId: '',
    qrContent: '',
    expiresAt: '',
    state: '',
    message: '',
    submitting: false,
  })

  // 更新检查结果只在本次运行内有效；关闭提示条只影响当前会话
  const updateInfo = reactive({
    current: '',
    latest: '',
    releaseName: '',
    releaseUrl: '',
    hasUpdate: false,
    checkedAt: '',
    skipped: false,
    dismissed: false,
    checking: false,
    error: '',
  })

  let eventsBound = false

  // 雨课堂、学习通与批改网的扫码事件结构一致，处理逻辑共用一份
  function bindQREvents(platform: QRPlatform) {
    const hint = qrHint(platform)

    EventsOn(`auth.${platform}.qr`, (ev: QRCodeEvent) => {
      if (login.platform !== platform || login.mode !== 'qr') return
      // 旧任务取消时可能晚到，已知当前 job 后只接收它的事件
      if (login.jobId && ev.jobId !== login.jobId) return
      login.qrContent = ev.qrContent
      login.expiresAt = ev.expiresAt
      login.state = 'waiting'
      login.message = hint
    })

    EventsOn(`auth.${platform}.state`, (ev: QRStateEvent) => {
      if (login.platform !== platform || login.mode !== 'qr') return
      if (login.jobId && ev.jobId !== login.jobId) return
      login.state = ev.state
      login.message = ev.message
      if (ev.state === 'succeeded') {
        window.setTimeout(() => {
          if (login.platform !== platform) return
          login.show = false
          login.qrContent = ''
        }, 800)
        void reloadAll()
      }
    })
  }

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

    bindQREvents('yuketang')
    bindQREvents('chaoxing')
    bindQREvents('pigai')

    EventsOn('account.state', (ev: AccountStateEvent) => {
      const data = bootstrap.value
      if (!data) return
      const item = data.platforms.find((p) => p.platform === ev.platform)
      if (item) item.loginState = ev.loginState
    })
  }

  // 按平台固定顺序归一，未知值丢弃；空列表回退为全部
  function normalizePlatforms(list: string[]): string[] {
    const valid = new Set(list.filter((p) => PLATFORM_ORDER.includes(p)))
    if (valid.size === 0) return [...PLATFORM_ORDER]
    return PLATFORM_ORDER.filter((p) => valid.has(p))
  }

  // 首次安装只记下当前版本，不弹窗；从旧版本升级时弹一次
  function syncChangelog(current: string, seen: string, firstRun: boolean) {
    if (!firstRun && seen !== current) {
      changelogShow.value = true
      return
    }
    if (!seen) {
      void markChangelogSeen()
        .then(() => {
          if (settings.value) settings.value.changelogSeenVersion = current
        })
        .catch(() => {
          // 记录失败不影响首次使用，下次启动再记
        })
    }
  }

  function openChangelog() {
    changelogShow.value = true
  }

  // 弹窗关闭后记录已读；失败时下次启动会再提示一次
  async function acknowledgeChangelog() {
    try {
      await markChangelogSeen()
      if (settings.value && bootstrap.value) {
        settings.value.changelogSeenVersion = bootstrap.value.version
      }
    } catch {
      // 记录失败不打断当前会话
    }
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
      enabledPlatforms.value = normalizePlatforms(settingsData.enabledPlatforms ?? [])
      syncChangelog(
        bootstrapData.version,
        settingsData.changelogSeenVersion,
        settingsData.firstRun,
      )
      await refreshList()
    } catch (err) {
      error.value = toErrorText(err)
    } finally {
      loading.value = false
    }
    // 首屏就绪后再查更新，失败静默，不拖慢启动
    void runUpdateCheck(false)
  }

  // manual 为 true 时失败抛错给设置页展示；自动检查失败只留状态
  async function runUpdateCheck(manual: boolean): Promise<UpdateCheckResult | null> {
    if (updateInfo.checking) return null
    updateInfo.checking = true
    updateInfo.error = ''
    try {
      const res = await checkForUpdate(manual)
      if (!res.ok) {
        updateInfo.error = res.error?.message ?? '检查更新失败'
        if (manual) throw new Error(updateInfo.error)
        return null
      }
      updateInfo.current = res.current || updateInfo.current
      updateInfo.latest = res.latest ?? ''
      updateInfo.releaseName = res.releaseName ?? ''
      updateInfo.releaseUrl = res.releaseUrl ?? ''
      updateInfo.hasUpdate = res.hasUpdate
      updateInfo.checkedAt = res.checkedAt ?? ''
      updateInfo.skipped = res.skipped === true
      // 手动检查发现新版时，重新显示本次运行里关过的提示条
      if (res.hasUpdate) updateInfo.dismissed = false
      return res
    } finally {
      updateInfo.checking = false
    }
  }

  async function checkUpdateManually() {
    return runUpdateCheck(true)
  }

  function dismissUpdate() {
    updateInfo.dismissed = true
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
        platforms: selectedPlatform.value ? [selectedPlatform.value] : undefined,
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

  // 点同一个平台取消筛选；其余开关（学期、已提交、搜索）继续生效
  function togglePlatformFilter(platform: string) {
    selectedPlatform.value = selectedPlatform.value === platform ? '' : platform
    void refreshList()
  }

  function clearPlatformFilter() {
    if (!selectedPlatform.value) return
    selectedPlatform.value = ''
    void refreshList()
  }

  // 勾选状态先写盘再更新界面，失败时保持原样
  async function setPlatformEnabled(platform: string, enabled: boolean) {
    const raw = enabled
      ? [...new Set([...enabledPlatforms.value, platform])]
      : enabledPlatforms.value.filter((p) => p !== platform)
    if (raw.length === 0) {
      throw new Error('至少保留一个参与刷新的平台')
    }
    const next = PLATFORM_ORDER.filter((p) => raw.includes(p))
    await saveEnabledPlatforms(next)
    enabledPlatforms.value = next
    if (settings.value) settings.value.enabledPlatforms = next
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

  async function beginLogin(platform: Exclude<LoginPlatform, ''>) {
    Object.assign(login, {
      show: true,
      platform,
      // 学习通默认走扫码，可以在弹窗里切到账号密码
      mode: 'qr',
      jobId: '',
      qrContent: '',
      expiresAt: '',
      state: 'waiting',
      message: '',
      submitting: false,
    })
    if (isQRPlatform(platform)) {
      login.message = '正在获取二维码…'
      const res = await startQRLogin(platform)
      login.jobId = res.jobId ?? ''
    }
  }

  // 学习通切到账号密码：先停掉扫码任务，避免后台继续轮询
  async function useChaoxingPassword() {
    if (login.platform !== 'chaoxing') return
    const jobId = login.jobId
    login.mode = 'password'
    login.jobId = ''
    login.qrContent = ''
    login.expiresAt = ''
    login.state = 'waiting'
    login.message = ''
    if (jobId) {
      try {
        await cancelQRLogin('chaoxing', jobId)
      } catch {
        // 任务可能已自然结束，取消失败无需打扰用户
      }
    }
  }

  // 学习通从账号密码切回扫码：重新起一个扫码任务
  async function useChaoxingQR() {
    if (login.platform !== 'chaoxing') return
    login.mode = 'qr'
    login.jobId = ''
    login.qrContent = ''
    login.expiresAt = ''
    login.state = 'waiting'
    login.message = '正在获取二维码…'
    const res = await startQRLogin('chaoxing')
    login.jobId = res.jobId ?? ''
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

  // 学在重邮只需验证并保存浏览器 session，失败原因回显在弹窗里
  async function submitImportSession(session: string) {
    login.submitting = true
    login.state = 'waiting'
    login.message = ''
    try {
      const res = await importTronclassSession({ session })
      if (!res.ok) {
        login.state = 'failed'
        login.message = res.error?.detail
          ? `${res.error.message}：${res.error.detail}`
          : res.error?.message ?? '导入失败'
        return false
      }
      login.state = 'succeeded'
      login.message = res.displayName ? `登录成功：${res.displayName}` : '登录成功'
      window.setTimeout(() => {
        if (login.platform === 'tronclass') {
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
    const platform = login.platform
    login.show = false
    login.platform = ''
    login.mode = 'qr'
    login.qrContent = ''
    login.expiresAt = ''
    login.state = ''
    login.message = ''
    login.jobId = ''
    login.submitting = false
    if (jobId && isQRPlatform(platform)) {
      try {
        await cancelQRLogin(platform, jobId)
      } catch {
        // 任务可能已自然结束，取消失败无需打扰用户
      }
    }
  }

  async function renewQR() {
    const platform = login.platform
    if (!login.jobId || !isQRPlatform(platform)) return
    // 先提示再请求：新码事件可能比 RPC 返回还早，晚写会把「等待扫码」覆盖掉
    login.message = '正在获取新的二维码…'
    await refreshQRLogin(platform, login.jobId)
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
    selectedPlatform,
    enabledPlatforms,
    changelogShow,
    refreshing,
    refreshJobId,
    runningPlatforms,
    progressByPlatform,
    lastRefreshError,
    login,
    updateInfo,
    init,
    reloadAll,
    refreshList,
    setStatus,
    setShowSubmitted,
    setTermOnly,
    togglePlatformFilter,
    clearPlatformFilter,
    setPlatformEnabled,
    openChangelog,
    acknowledgeChangelog,
    checkUpdateManually,
    dismissUpdate,
    beginRefresh,
    stopRefresh,
    beginLogin,
    useChaoxingPassword,
    useChaoxingQR,
    submitPasswordLogin,
    submitImportSession,
    stopLogin,
    renewQR,
  }
})
