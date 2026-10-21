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
}

export interface AssignmentView {
  id: number
  platform: string
  courseName: string
  courseExternalId: string
  title: string
  status: 'submitted' | 'unsubmitted' | 'unknown'
  phase: 'open' | 'closed' | 'unknown'
  deadlineAt?: string
  deadlineLocal?: string
  deadlineSource: string
  deadlineRaw?: string
  detailUrl?: string
  firstSeenAt: string
  lastSeenAt: string
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
