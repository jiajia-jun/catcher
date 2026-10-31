// 新增 AI 绑定集中于此；不手工修改 Wails 生成文件。
export interface AISettings {
  enabled: boolean
  baseUrl: string
  model: string
  maxInputChars: number
  maxOutputTokens: number
  monthlyTokenLimit: number
  timeoutSeconds: number
}
export interface AISource {
  id: number; ref: string; platform: string; course: string; title: string
  status: string; platformStatus: string; manual: boolean
  deadline: string | null; deadlineSource: string; lastSeenAt: string
}
export interface AICoverage { platform: string; seenAt: string | null; complete: boolean; warnings: number }
export interface StudyTask {
  id: string; assignmentId: number; title: string; startAt: string; minutes: number
  steps: string[]; reason: string; done: boolean
}
export interface StudyPlan {
  id: string; title: string; tasks: StudyTask[]; notes: string[]; sources: AISource[]
  sourceIds: number[]; fingerprint: string; startAt: string; endAt: string
  blocked: { startAt: string; endAt: string }[]; createdAt: string; stale: boolean
}
export interface AIJob {
  id: string; kind: string; state: string; error?: string; sources: AISource[]
  coverage: AICoverage[]; notes: string[]; plan?: StudyPlan
  inputTokens: number; outputTokens: number; usageKnown: boolean
}
export interface AIInput {
  kind: 'query' | 'plan' | 'test'; question: string; assignmentIds: number[]
  startAt: string; endAt: string; snapshot?: string
  blocked: { startAt: string; endAt: string }[]
}
export interface AIPreview {
  snapshot: string; sources: AISource[]; coverage: AICoverage[]
  payload: string; system: string; endpoint: string; model: string
}
export interface AIState {
  settings: AISettings; hasKey: boolean
  usage: { month: string; chargedTokens: number; reservedTokens: number; runs: number }
}

// 直接调用 Wails 运行时导出的同名方法，保持生成绑定由 Wails 自身维护。
export async function aiCall<T>(name: string, ...args: unknown[]): Promise<T> {
  const app = (window as unknown as {
    go?: { handle?: { App?: Record<string, (...values: unknown[]) => Promise<{ ok: boolean; data?: T; error?: { message: string } }>> } }
  }).go?.handle?.App
  if (!app?.[name]) throw new Error('AI 运行时不可用，请通过新版 Catcher 或 wails dev 启动')
  const res = await app[name](...args)
  if (!res.ok || !res.data) throw new Error(res.error?.message ?? 'AI 操作失败')
  return res.data
}
