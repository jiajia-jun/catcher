import {
  CancelChaoxingLogin,
  CancelRefresh,
  CancelYuketangLogin,
  CheckForUpdate,
  GetBootstrap,
  GetEmailSettings,
  GetFetchIssues,
  GetSettings,
  ImportTronclassSession,
  ListAssignments,
  LoginChaoxing,
  OpenDataDir,
  RefreshChaoxingLogin,
  RefreshYuketangLogin,
  SaveEmailSettings,
  SendTestEmail,
  SetAssignmentStatus,
  StartChaoxingLogin,
  StartRefresh,
  StartYuketangLogin,
} from '../../wailsjs/go/handle/App'
import type {
  APIError,
  APIResult,
  AssignmentFilter,
  AssignmentListData,
  AssignmentListResult,
  AssignmentStatusResult,
  AssignmentView,
  BootstrapData,
  BootstrapResult,
  EmailSettingsRequest,
  EmailSettingsResult,
  EmailSettingsView,
  EmailTestRequest,
  FetchIssuesResult,
  PasswordLoginResult,
  RefreshStartResult,
  SessionImportRequest,
  SessionImportResult,
  SettingsResult,
  SettingsView,
  UpdateCheckResult,
} from '../types'

function ensureRuntime() {
  if (!(window as any).go?.handle?.App) {
    throw new Error('Wails 运行时不可用，请通过 wails dev 或 Catcher.exe 启动')
  }
}

function errorText(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function apiFailure(error?: APIError): Error {
  if (!error) return new Error('未知错误')
  return new Error(error.detail ? `${error.message}：${error.detail}` : error.message)
}

export async function loadBootstrap(termOnly: boolean): Promise<BootstrapData> {
  ensureRuntime()
  const res = (await GetBootstrap(termOnly)) as BootstrapResult
  if (!res?.ok || !res.data) throw apiFailure(res?.error)
  return res.data
}

export async function loadAssignments(filter: AssignmentFilter): Promise<AssignmentListData> {
  ensureRuntime()
  const res = (await ListAssignments(filter)) as AssignmentListResult
  if (!res?.ok) throw apiFailure(res?.error)
  return {
    ...res,
    items: res.items ?? [],
    total: res.total ?? 0,
  }
}

// status 传空串表示恢复平台值
export async function setAssignmentStatus(
  id: number,
  status: 'submitted' | 'unsubmitted' | '',
): Promise<AssignmentView> {
  ensureRuntime()
  const res = (await SetAssignmentStatus({ id, status })) as AssignmentStatusResult
  if (!res?.ok || !res.item) throw apiFailure(res?.error)
  return res.item
}

export async function loadSettings(): Promise<SettingsView> {
  ensureRuntime()
  const res = (await GetSettings()) as SettingsResult
  if (!res?.ok || !res.settings) throw apiFailure(res?.error)
  return res.settings
}

export async function loadEmailSettings(): Promise<EmailSettingsView> {
  ensureRuntime()
  const res = (await GetEmailSettings()) as EmailSettingsResult
  if (!res?.ok || !res.settings) throw apiFailure(res?.error)
  return res.settings
}

// 保存后返回最新视图，password 留空时后端沿用已保存的授权码
export async function saveEmailSettings(
  input: EmailSettingsRequest,
): Promise<EmailSettingsView> {
  ensureRuntime()
  const res = (await SaveEmailSettings(input)) as EmailSettingsResult
  if (!res?.ok || !res.settings) throw apiFailure(res?.error)
  return res.settings
}

// 测试邮件发送到发件人自己；失败必须让调用方看到
export async function sendTestEmail(input: EmailTestRequest): Promise<void> {
  ensureRuntime()
  const res = (await SendTestEmail(input)) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

export async function openDataDir(): Promise<void> {
  ensureRuntime()
  const res = (await OpenDataDir()) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

// 更新检查失败通过返回值展示；manual 为 false 时由调用方静默处理
export async function checkForUpdate(manual: boolean): Promise<UpdateCheckResult> {
  ensureRuntime()
  return (await CheckForUpdate(manual)) as UpdateCheckResult
}

export async function startRefresh(platforms?: string[]): Promise<RefreshStartResult> {
  ensureRuntime()
  const res = (await StartRefresh(platforms ?? [])) as RefreshStartResult
  if (!res?.ok) throw apiFailure(res?.error)
  return res
}

export async function cancelRefresh(jobId: string): Promise<void> {
  ensureRuntime()
  const res = (await CancelRefresh(jobId)) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

// 支持扫码的平台。雨课堂与学习通的二维码都走服务端下发的图片
export type QRPlatform = 'yuketang' | 'chaoxing'

export async function startQRLogin(platform: QRPlatform): Promise<RefreshStartResult> {
  ensureRuntime()
  const res = (platform === 'chaoxing'
    ? await StartChaoxingLogin()
    : await StartYuketangLogin()) as RefreshStartResult
  if (!res?.ok) throw apiFailure(res?.error)
  return res
}

export interface PasswordLoginInput {
  phone: string
  password: string
  remember: boolean
}

// 登录失败需要通过返回值展示在弹窗里，这里不抛异常
export async function loginChaoxing(input: PasswordLoginInput): Promise<PasswordLoginResult> {
  ensureRuntime()
  return (await LoginChaoxing(input)) as PasswordLoginResult
}

// 导入失败同样通过返回值展示，不抛异常
export async function importTronclassSession(
  input: SessionImportRequest,
): Promise<SessionImportResult> {
  ensureRuntime()
  return (await ImportTronclassSession(input)) as SessionImportResult
}

export async function cancelQRLogin(platform: QRPlatform, jobId: string): Promise<void> {
  ensureRuntime()
  const res = (platform === 'chaoxing'
    ? await CancelChaoxingLogin(jobId)
    : await CancelYuketangLogin(jobId)) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

export async function refreshQRLogin(platform: QRPlatform, jobId: string): Promise<void> {
  ensureRuntime()
  const res = (platform === 'chaoxing'
    ? await RefreshChaoxingLogin(jobId)
    : await RefreshYuketangLogin(jobId)) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

export async function loadFetchIssues(runId: number): Promise<FetchIssuesResult> {
  ensureRuntime()
  const res = (await GetFetchIssues(runId)) as FetchIssuesResult
  if (!res?.ok) throw apiFailure(res?.error)
  return { ...res, items: res.items ?? [] }
}

export function toErrorText(err: unknown): string {
  return errorText(err)
}
