// 仅由 Vite 开发服务器手动打开的 UI 验收夹具，不被生产 index.html 引用。
// 全部数据与 Key 为模拟值；没有任何云端或平台网络请求。
import { createApp, h, ref } from 'vue'
import { NConfigProvider } from 'naive-ui'
import AIStudyModal from '../src/components/AIStudyModal.vue'

const now = new Date()
const deadline = new Date(now.getTime() + 3 * 86400000).toISOString()
const sources = [
  { id: 1, ref: 'A1', platform: 'chaoxing', course: '高等数学', title: '习题整理', status: 'unsubmitted', platformStatus: 'unsubmitted', manual: false, deadline, deadlineSource: 'platform_exact', lastSeenAt: now.toISOString() },
  { id: 2, ref: 'A2', platform: 'yuketang', course: '英语', title: '阅读准备', status: 'unknown', platformStatus: 'unknown', manual: false, deadline: null, deadlineSource: 'unknown', lastSeenAt: now.toISOString() },
]
let config = { enabled: false, baseUrl: 'https://example.invalid', model: 'mock-json', maxInputChars: 12000, maxOutputTokens: 2048, monthlyTokenLimit: 200000, timeoutSeconds: 60 }
let hasKey = false
let job: any = null
let lastPreview: any = null
const plans: any[] = []
const success = (data: unknown) => Promise.resolve({ ok: true, data: JSON.parse(JSON.stringify(data)) })
const state = () => ({ settings: config, hasKey, usage: { month: '模拟月份', chargedTokens: 0, reservedTokens: 0, runs: 0 } })
const mock: Record<string, (...args: any[]) => any> = {
  ListAssignments: async () => ({ ok: true, items: sources.map(a => ({ id: a.id, platform: a.platform, courseName: a.course, title: a.title, status: a.status })), total: 2 }),
  GetAISettings: () => success(state()),
  SaveAISettings: req => { config = { ...req, apiKey: undefined, clearKey: undefined } as any; hasKey = !req.clearKey && (hasKey || !!req.apiKey); return success(state()) },
  ListStudyPlans: () => success({ plans }),
  PreviewAI: req => { const chosen = req.kind === 'test' ? [] : sources.filter(a => req.assignmentIds.includes(a.id)); lastPreview = { ...req, sources: chosen }; return success({ snapshot: 'mock-preview', sources: chosen, coverage: [], payload: JSON.stringify({ question: req.question, assignments: chosen, startAt: req.startAt, endAt: req.endAt }, null, 2), system: '本地 UI 验收模拟上下文；不发送到任何服务器。', endpoint: config.baseUrl, model: config.model }) },
  StartAI: req => {
    job = { id: 'mock-job', kind: req.kind, state: 'succeeded', sources: lastPreview.sources, notes: ['这是 UI 模拟结果；真实模型效果未验证', '英语提交状态与截止时间未知，需自行核对'], coverage: [], usageKnown: true, inputTokens: 120, outputTokens: 50 }
    if (req.kind === 'plan') job.plan = { id: 'mock-plan', title: '今晚学习安排', sourceIds: req.assignmentIds, sources: lastPreview.sources, fingerprint: 'mock', startAt: req.startAt, endAt: req.endAt, createdAt: now.toISOString(), blocked: [], notes: ['时长为估算；完成学习子任务不代表已在平台提交'], tasks: lastPreview.sources.map((a: any, i: number) => ({ id: `task-${i}`, assignmentId: a.id, title: `准备：${a.title}`, startAt: new Date(new Date(req.startAt).getTime() + i * 30 * 60000).toISOString(), minutes: 30, reason: '仅基于标题提供准备步骤，需核对实际题目', steps: ['打开原平台核对作业要求', '整理待完成事项'], done: false })) }
    return success({ jobId: job.id })
  },
  GetAIJob: () => success(job), CancelAI: () => success({}),
  ConfirmStudyPlan: req => { if (!plans.some(p => p.id === job.plan.id)) plans.push({ ...job.plan, tasks: JSON.parse(JSON.stringify(req.tasks)) }); return success(job.plan) },
  SetStudyTaskDone: req => { const task = plans.find(p => p.id === req.planId)?.tasks.find((t: any) => t.id === req.taskId); if (task) task.done = req.done; return success({}) },
}
;(window as any).go = { handle: { App: mock } }
const modalShow = ref(true)
createApp({ render: () => h(NConfigProvider, null, { default: () => [
  h('p', '仅本地模拟数据验收，不连接模型或作业平台。'),
  h(AIStudyModal, { show: modalShow.value, 'onUpdate:show': value => { modalShow.value = value } }),
] }) }).mount('#app')
