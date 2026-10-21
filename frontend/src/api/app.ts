import {
  GetBootstrap,
  GetSettings,
  ListAssignments,
  OpenDataDir,
} from '../../wailsjs/go/handle/App'
import type {
  APIError,
  APIResult,
  AssignmentFilter,
  AssignmentListData,
  AssignmentListResult,
  BootstrapData,
  BootstrapResult,
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

export async function loadBootstrap(): Promise<BootstrapData> {
  ensureRuntime()
  const res = (await GetBootstrap()) as BootstrapResult
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

export function toErrorText(err: unknown): string {
  return errorText(err)
}
