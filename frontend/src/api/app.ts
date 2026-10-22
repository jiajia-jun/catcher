import {
  CancelRefresh,
  CancelYuketangLogin,
  GetBootstrap,
  GetFetchIssues,
  GetSettings,
  ListAssignments,
  LoginChaoxing,
  OpenDataDir,
  RefreshYuketangLogin,
  StartRefresh,
  StartYuketangLogin,
} from '../../wailsjs/go/handle/App'
import type {
  APIError,
  APIResult,
  AssignmentFilter,
  AssignmentListData,
  AssignmentListResult,
  BootstrapData,
  BootstrapResult,
  FetchIssuesResult,
  PasswordLoginResult,
  RefreshStartResult,
  SettingsResult,
  SettingsView,
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

export async function loadSettings(): Promise<SettingsView> {
  ensureRuntime()
  const res = (await GetSettings()) as SettingsResult
  if (!res?.ok || !res.settings) throw apiFailure(res?.error)
  return res.settings
}

export async function openDataDir(): Promise<void> {
  ensureRuntime()
  const res = (await OpenDataDir()) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
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

export async function startYuketangLogin(): Promise<RefreshStartResult> {
  ensureRuntime()
  const res = (await StartYuketangLogin()) as RefreshStartResult
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

export async function cancelYuketangLogin(jobId: string): Promise<void> {
  ensureRuntime()
  const res = (await CancelYuketangLogin(jobId)) as APIResult
  if (!res?.ok) throw apiFailure(res?.error)
}

export async function refreshYuketangLogin(jobId: string): Promise<void> {
  ensureRuntime()
  const res = (await RefreshYuketangLogin(jobId)) as APIResult
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
