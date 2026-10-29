export interface APIError {
  code: string
  message: string
  detail?: string
  rawPath?: string
}

export interface CountsView {
  openUnsubmitted: number
  overdueUnsubmitted: number
  unknown: number
  submitted: number
}

export interface PlatformView {
  platform: string
  displayName: string
  loginState: string
  lastRunAt?: string
  lastRunState?: string
  lastRunId?: number
  counts: CountsView
  error?: APIError
  experimental: boolean
}

export interface BootstrapData {
  platforms: PlatformView[]
  stats: CountsView
  dataDir: string
  version: string
}

export interface BootstrapResult {
  ok: boolean
  error?: APIError
  data?: BootstrapData
}

export interface AssignmentFilter {
  platforms?: string[]
  courseIds?: number[]
  statuses?: string[]
  showSubmitted: boolean
  keyword?: string
  // 只看本学期（后端按 9 月 1 日划线）
  termOnly: boolean
}

export interface AssignmentView {
  id: number
  platform: string
  courseName: string
  courseExternalId: string
  title: string
  // 有效状态：有手动值用手动值
  status: 'submitted' | 'unsubmitted' | 'unknown'
  // 平台给出的原始状态
  platformStatus: 'submitted' | 'unsubmitted' | 'unknown'
  manualStatus?: 'submitted' | 'unsubmitted'
  manualStatusAt?: string
  phase: 'open' | 'closed' | 'unknown'
  deadlineAt?: string
  deadlineLocal?: string
  deadlineSource: string
  deadlineRaw?: string
  detailUrl?: string
  firstSeenAt: string
  lastSeenAt: string
}

export interface AssignmentStatusResult {
  ok: boolean
  item?: AssignmentView
  error?: APIError
}

export interface AssignmentListResult {
  ok: boolean
  error?: APIError
  items: AssignmentView[] | null
  total: number
}

export interface AssignmentListData {
  items: AssignmentView[]
  total: number
}

export interface SettingsView {
  logLevel: string
  rawEnabled: boolean
  rawRetentionDays: number
  includeHistory: boolean
  theme: string
  dataDir: string
  version: string
}

export interface SettingsResult {
  ok: boolean
  error?: APIError
  settings?: SettingsView
}

export interface APIResult {
  ok: boolean
  error?: APIError
}

// 一次版本检查的结果；skipped 表示开发模式跳过了自动检查
export interface UpdateCheckResult {
  ok: boolean
  current: string
  latest?: string
  releaseName?: string
  releaseUrl?: string
  hasUpdate: boolean
  checkedAt?: string
  skipped?: boolean
  error?: APIError
}

export interface RefreshStartResult {
  ok: boolean
  jobId?: string
  platforms?: string[]
  alreadyRunning: boolean
  error?: APIError
}

export interface FetchIssueView {
  scope: string
  ref?: string
  kind: string
  message: string
  rawPath?: string
}

export interface FetchIssuesResult {
  ok: boolean
  items: FetchIssueView[] | null
  error?: APIError
}

export interface RefreshProgressEvent {
  jobId: string
  platform: string
  phase: string
  current: number
  total: number
  message: string
}

export interface RefreshIssueEvent {
  scope: string
  ref?: string
  kind: string
  message: string
  rawPath?: string
}

export interface RefreshErrorEvent {
  code: string
  message: string
}

export interface RefreshFinishedEvent {
  jobId: string
  platform: string
  status: 'success' | 'partial' | 'failed' | 'canceled'
  counts: {
    coursesTotal: number
    coursesOk: number
    itemsSeen: number
    itemsUpserted: number
    warningCount: number
  }
  warnings?: RefreshIssueEvent[]
  error?: RefreshErrorEvent
}

export interface RefreshAllFinishedEvent {
  jobId: string
  statuses: Record<string, string>
}

export interface QRCodeEvent {
  jobId: string
  qrContent: string
  expiresAt: string
}

export interface QRStateEvent {
  jobId: string
  state: 'waiting' | 'scanned' | 'succeeded' | 'expired' | 'failed'
  message: string
}

export interface AccountStateEvent {
  platform: string
  loginState: string
  error?: RefreshErrorEvent
}

export interface SessionImportRequest {
  session: string
}

export interface SessionImportResult {
  ok: boolean
  loginState?: string
  displayName?: string
  error?: APIError
}

export interface PasswordLoginResult {
  ok: boolean
  loginState?: string
  displayName?: string
  error?: APIError
}
