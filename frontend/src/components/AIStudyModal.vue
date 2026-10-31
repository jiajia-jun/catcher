<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  NAlert, NButton, NCard, NCheckbox, NCollapse, NCollapseItem, NEmpty,
  NForm, NFormItem, NInput, NInputNumber, NModal, NSelect, NSpace,
  NSpin, NSwitch, NTabPane, NTabs, NTag,
} from 'naive-ui'
import { aiCall } from '../api/ai'
import type { AIInput, AIJob, AIPreview, AISettings, AISource, AIState, StudyPlan } from '../api/ai'
import { loadAssignments } from '../api/app'
import type { AssignmentView } from '../types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (event: 'update:show', value: boolean): void }>()
const visible = computed({ get: () => props.show, set: value => emit('update:show', value) })
const tab = ref('assistant')
const error = ref('')
const notice = ref('')
const busy = ref(false)
const running = ref(false)
const canceling = ref(false)
const currentJob = ref('')
const settings = ref<AISettings>({ enabled: false, baseUrl: 'https://api.deepseek.com', model: 'deepseek-flash', maxInputChars: 12000, maxOutputTokens: 2048, monthlyTokenLimit: 200000, timeoutSeconds: 60 })
const apiKey = ref('')
const hasKey = ref(false)
const usage = ref<AIState['usage'] | null>(null)
const assignments = ref<AssignmentView[]>([])
const selected = ref<number[]>([])
const question = ref('')
const kind = ref<'query' | 'plan'>('plan')
const starts = ref('')
const ends = ref('')
const blocked = ref<{ start: string; end: string }[]>([])
const preview = ref<AIPreview | null>(null)
const consent = ref(false)
const result = ref<AIJob | null>(null)
const plans = ref<StudyPlan[]>([])
const selectedSource = ref<AISource | null>(null)
let pollTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let generation = 0

const platformNames: Record<string, string> = { chaoxing: '学习通', yuketang: '雨课堂', tronclass: '学在重邮', pigai: '批改网' }
const statusNames: Record<string, string> = { submitted: '已提交', unsubmitted: '未提交', unknown: '状态未知' }
const deadlineSources: Record<string, string> = { platform_exact: '平台给出', detail_exact: '详情页', derived_remaining: '剩余时间推算', inferred_year: '年份推断', unknown: '未知' }
const options = computed(() => assignments.value.map(a => ({ label: `${platformNames[a.platform] ?? a.platform} · ${a.courseName} · ${a.title}`, value: a.id })))
const inputSignature = computed(() => JSON.stringify([selected.value, question.value, kind.value, starts.value, ends.value, blocked.value]))
watch(inputSignature, () => {
  preview.value = null; consent.value = false
  if (!running.value) result.value = null
})
watch(() => props.show, value => { if (value) void initialize(); else apiKey.value = '' }, { immediate: true })

function textError(err: unknown) { return err instanceof Error ? err.message : String(err) }
function localInput(date: Date) { const pad = (n: number) => String(n).padStart(2, '0'); return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}` }
function formatTime(raw: string | null | undefined) { return raw ? new Date(raw).toLocaleString('zh-CN', { hour12: false }) : '未知' }
function iso(value: string) { const at = new Date(value); if (Number.isNaN(at.getTime())) throw new Error('请填写有效时间'); return at.toISOString() }
function input(test = false): AIInput {
  return { kind: test ? 'test' : kind.value, question: test ? '' : question.value, assignmentIds: test ? [] : [...selected.value], startAt: !test && kind.value === 'plan' ? iso(starts.value) : '', endAt: !test && kind.value === 'plan' ? iso(ends.value) : '', blocked: !test && kind.value === 'plan' ? blocked.value.map(b => ({ startAt: iso(b.start), endAt: iso(b.end) })) : [] }
}
async function refreshState() {
  const state = await aiCall<AIState>('GetAISettings')
  settings.value = state.settings; hasKey.value = state.hasKey; usage.value = state.usage
}
async function refreshPlans() { plans.value = (await aiCall<{ plans: StudyPlan[] }>('ListStudyPlans')).plans ?? [] }
async function initialize() {
  error.value = ''; busy.value = true
  try {
    const list = await loadAssignments({ showSubmitted: true, termOnly: true })
    assignments.value = list.items
    const valid = new Set(list.items.map(a => a.id)); selected.value = selected.value.filter(id => valid.has(id))
    await Promise.all([refreshState(), refreshPlans()])
    if (!starts.value) { const at = new Date(); at.setMinutes(at.getMinutes() + 10, 0, 0); starts.value = localInput(at); ends.value = localInput(new Date(at.getTime() + 2 * 3600000)) }
  } catch (err) { error.value = textError(err) } finally { busy.value = false }
}
async function saveSettings(clear = false) {
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const state = await aiCall<AIState>('SaveAISettings', { ...settings.value, enabled: clear ? false : settings.value.enabled, apiKey: apiKey.value, clearKey: clear })
    settings.value = state.settings; hasKey.value = state.hasKey; usage.value = state.usage
    apiKey.value = ''; preview.value = null; consent.value = false
    notice.value = clear ? '已关闭 AI 并清除本机 Key' : 'AI 设置已保存；只有点击发送才会调用模型'
  } catch (err) { error.value = textError(err) } finally { busy.value = false }
}
async function makePreview(test = false) {
  error.value = ''; notice.value = ''; busy.value = true; consent.value = false
  try { preview.value = await aiCall<AIPreview>('PreviewAI', input(test)) } catch (err) { error.value = textError(err) } finally { busy.value = false }
}
async function send() {
  if (!preview.value || !consent.value || running.value) return
  error.value = ''; result.value = null; running.value = true; canceling.value = false
  const token = ++generation
  try {
    // 连接测试的预览没有作业，完全根据预览种类发送。
    const isTest = preview.value.sources.length === 0
    const response = await aiCall<{ jobId: string }>('StartAI', { ...input(isTest), snapshot: preview.value.snapshot })
    currentJob.value = response.jobId
    preview.value = null; consent.value = false
    void poll(response.jobId, token)
  } catch (err) { error.value = textError(err); running.value = false }
}
async function poll(id: string, token: number) {
  if (disposed || token !== generation) return
  try {
    const job = await aiCall<AIJob>('GetAIJob', id)
    if (disposed || token !== generation) return
    if (job.state === 'running') { pollTimer = setTimeout(() => { void poll(id, token) }, 500); return }
    result.value = job; running.value = false; canceling.value = false; currentJob.value = ''
    if (job.error) error.value = job.error
    try { await refreshState() } catch (err) { error.value ||= textError(err) }
  } catch (err) { error.value = textError(err); running.value = false; canceling.value = false }
}
async function cancel() {
  if (!currentJob.value || canceling.value) return
  canceling.value = true
  try { await aiCall('CancelAI', currentJob.value) } catch (err) { error.value = textError(err); canceling.value = false }
}
async function confirm() {
  if (!result.value?.plan) return
  busy.value = true; error.value = ''
  try {
    await aiCall('ConfirmStudyPlan', { jobId: result.value.id, tasks: result.value.plan.tasks })
    notice.value = '学习计划已保存；不会修改平台作业的提交状态'
    result.value = null; await refreshPlans(); tab.value = 'plans'
  } catch (err) { error.value = textError(err) } finally { busy.value = false }
}
function move(index: number, delta: number) {
  const tasks = result.value?.plan?.tasks; if (!tasks) return
  const to = index + delta; if (to < 0 || to >= tasks.length) return
  const [task] = tasks.splice(index, 1); tasks.splice(to, 0, task)
}
async function setDone(plan: StudyPlan, taskID: string, done: boolean) {
  try { await aiCall('SetStudyTaskDone', { planId: plan.id, taskId: taskID, done }); await refreshPlans() } catch (err) { error.value = textError(err) }
}
function replan(plan: StudyPlan) {
  selected.value = plan.sourceIds.filter(id => assignments.value.some(a => a.id === id))
  question.value = `根据最新作业情况重新安排：${plan.title}`; kind.value = 'plan'; tab.value = 'assistant'; result.value = null
  const at = new Date(Date.now() + 10 * 60000); starts.value = localInput(at); ends.value = localInput(new Date(at.getTime() + 2 * 3600000)); blocked.value = []
}
function sourceFor(id: number, sources: AISource[] = result.value?.sources ?? []) { return sources.find(a => a.id === id) }
onBeforeUnmount(() => {
  disposed = true; generation++; if (pollTimer) clearTimeout(pollTimer)
  if (currentJob.value) void aiCall('CancelAI', currentJob.value).catch(() => {})
  apiKey.value = ''
})
</script>

<template>
  <n-modal v-model:show="visible" :mask-closable="!running" :close-on-esc="!running">
    <n-card class="ai-dialog" title="AI 学习助手" :bordered="false" role="dialog" aria-modal="true">
      <template #header-extra><n-button quaternary @click="visible = false">收起</n-button></template>
      <n-alert type="info" :bordered="false" class="gap">云端 AI 默认关闭。开启后，仅在你预览并发送时向所选服务商发送问题、所选作业元数据和时间安排。不会发送平台密码、Cookie 或登录态。</n-alert>
      <n-alert v-if="error" type="error" class="gap" closable @close="error = ''">{{ error }}</n-alert>
      <n-alert v-if="notice" type="success" class="gap" closable @close="notice = ''">{{ notice }}</n-alert>
      <n-alert v-if="running" type="info" class="gap"><n-space align="center"><span>AI 请求进行中，收起窗口后仍可查看结果。</span><n-button size="small" :loading="canceling" @click="cancel">取消请求</n-button></n-space></n-alert>
      <n-spin :show="busy">
        <n-tabs v-model:value="tab" type="line">
          <n-tab-pane name="assistant" tab="查询与计划">
            <n-space vertical :size="14">
              <n-space align="center"><n-tag :type="settings.enabled ? 'success' : 'default'">AI {{ settings.enabled ? '已开启' : '已关闭' }}</n-tag><n-button size="small" @click="tab = 'settings'">模型设置</n-button></n-space>
              <n-form label-placement="top">
                <n-form-item label="选择本次允许 AI 处理的作业（最多 40 条，只查询这些记录）">
                  <n-select v-model:value="selected" multiple filterable :options="options" :disabled="running" placeholder="搜索课程或作业后选择" />
                </n-form-item>
                <n-space class="gap"><n-button size="small" :disabled="running" @click="selected = assignments.filter(a => a.status !== 'submitted').slice(0, 40).map(a => a.id)">选择前 40 条未完成 / 状态未知作业</n-button><n-button size="small" :disabled="running" @click="selected = []">清空选择</n-button><n-button size="small" :disabled="running" @click="initialize">重新读取本地作业</n-button></n-space>
                <n-form-item label="要做什么"><n-select v-model:value="kind" :disabled="running" :options="[{ label: '安排学习计划', value: 'plan' }, { label: '查询作业', value: 'query' }]" /></n-form-item>
                <n-form-item label="问题、课程偏好与要求"><n-input v-model:value="question" type="textarea" :maxlength="2000" show-count :disabled="running" :placeholder="kind === 'plan' ? '例如：数学优先，帮我安排这些作业；时长只能估算的请说明假设' : '例如：这周还有哪些未完成的数学作业？'" /></n-form-item>
                <template v-if="kind === 'plan'">
                  <div class="time-grid"><n-form-item label="开始时间（本机时区）"><input v-model="starts" type="datetime-local" :disabled="running" aria-label="学习开始时间" /></n-form-item><n-form-item label="结束时间（最长 12 小时）"><input v-model="ends" type="datetime-local" :disabled="running" aria-label="学习结束时间" /></n-form-item></div>
                  <p class="muted">固定安排请添加到下面；仅写在文字里的时间限制不能保证被程序校验。</p>
                  <div v-for="(item, i) in blocked" :key="i" class="time-grid gap"><input v-model="item.start" type="datetime-local" :disabled="running" aria-label="固定安排开始时间" /><input v-model="item.end" type="datetime-local" :disabled="running" aria-label="固定安排结束时间" /><n-button size="small" :disabled="running" @click="blocked.splice(i, 1)">移除</n-button></div>
                  <n-button size="small" :disabled="running || blocked.length >= 20" @click="blocked.push({ start: starts, end: ends })">添加固定安排</n-button>
                </template>
              </n-form>
              <n-button type="primary" :disabled="running || !settings.enabled || !selected.length" @click="makePreview()">预览发送内容</n-button>
              <n-alert type="warning" :bordered="false">完整题干覆盖有限，AI 步骤和时长是建议。未知截止时间或提交状态保持未知；本地学习任务完成不代表已在平台提交。</n-alert>
            </n-space>
          </n-tab-pane>
          <n-tab-pane name="plans" tab="已保存计划">
            <n-empty v-if="!plans.length" description="还没有已确认的学习计划" />
            <n-card v-for="plan in plans" :key="plan.id" :title="plan.title" size="small" class="gap">
              <n-alert v-if="plan.stale" type="warning" class="gap">作业或会话已变化，保存的安排仅供参考。可以重新生成草案。</n-alert>
              <p>{{ formatTime(plan.startAt) }} — {{ formatTime(plan.endAt) }}</p>
              <div v-for="task in plan.tasks" :key="task.id" class="saved-task">
                <n-checkbox :checked="task.done" @update:checked="setDone(plan, task.id, $event)">{{ task.title }}</n-checkbox>
                <p class="muted">{{ formatTime(task.startAt) }} · {{ task.minutes }} 分钟（估算） · {{ sourceFor(task.assignmentId, plan.sources)?.course }}</p>
                <n-button text type="primary" @click="selectedSource = sourceFor(task.assignmentId, plan.sources) ?? null">查看原作业依据</n-button>
              </div>
              <p v-for="(note, i) in plan.notes" :key="i" class="muted">{{ note }}</p>
              <n-button :disabled="running" @click="replan(plan)">根据最新数据重新安排</n-button>
            </n-card>
          </n-tab-pane>
          <n-tab-pane name="settings" tab="AI 设置">
            <n-form label-placement="top">
              <n-form-item label="主动开启云端 AI"><n-switch v-model:value="settings.enabled" :disabled="running" /></n-form-item>
              <n-form-item label="模型服务基地址（HTTPS，支持 Chat Completions JSON 输出）"><n-input v-model:value="settings.baseUrl" placeholder="https://api.deepseek.com" :disabled="running" /></n-form-item>
              <n-form-item label="模型名称"><n-input v-model:value="settings.model" :disabled="running" /></n-form-item>
              <n-form-item :label="hasKey ? 'API Key（已加密保存；留空保持原值）' : 'API Key（仅加密保存在本机）'"><n-input v-model:value="apiKey" type="password" placeholder="填写服务商提供的 API Key" show-password-on="click" autocomplete="off" :disabled="running" /></n-form-item>
              <div class="limits-grid">
                <n-form-item label="单次输入字符上限"><n-input-number v-model:value="settings.maxInputChars" :min="500" :max="30000" :disabled="running" /></n-form-item>
                <n-form-item label="单次输出 Token 上限"><n-input-number v-model:value="settings.maxOutputTokens" :min="256" :max="8192" :disabled="running" /></n-form-item>
                <n-form-item label="本月 Token 额度"><n-input-number v-model:value="settings.monthlyTokenLimit" :min="1000" :max="10000000" :disabled="running" /></n-form-item>
                <n-form-item label="请求超时（秒）"><n-input-number v-model:value="settings.timeoutSeconds" :min="10" :max="120" :disabled="running" /></n-form-item>
              </div>
              <p v-if="usage" class="muted">{{ usage.month }}：已计入 {{ usage.chargedTokens }} Token；在途预留 {{ usage.reservedTokens }}；{{ usage.runs }} 次请求。无用量回执的请求按预留保守计入，实际账单以服务商为准。</p>
              <n-space><n-button type="primary" :disabled="running" @click="saveSettings()">保存设置</n-button><n-button :disabled="running || !settings.enabled || !hasKey" @click="makePreview(true)">预览连接测试</n-button><n-button type="error" secondary :disabled="running" @click="saveSettings(true)">关闭并清除 Key</n-button></n-space>
              <p class="muted">保存后再测试。连接测试也会调用模型并可能计费；默认不会自动重试或切换服务商。</p>
            </n-form>
          </n-tab-pane>
        </n-tabs>
        <n-card v-if="preview" title="确认本次发送范围" size="small" class="gap preview">
          <p>目标：{{ preview.endpoint }} · 模型：{{ preview.model }} · 作业：{{ preview.sources.length }} 条</p>
          <n-collapse><n-collapse-item title="查看将发送的内容与系统约束" name="payload"><pre>{{ preview.payload }}</pre><pre>{{ preview.system }}</pre></n-collapse-item></n-collapse>
          <n-checkbox v-model:checked="consent" class="gap">我同意将以上内容发送给所选模型服务商，并使用本机配置的额度</n-checkbox>
          <n-space><n-button type="primary" :disabled="!consent || running" @click="send">确认发送</n-button><n-button @click="preview = null; consent = false">取消预览</n-button></n-space>
        </n-card>
        <n-card v-if="result && result.state === 'succeeded'" :title="result.plan ? '计划草案：' + result.plan.title : 'AI 查询结果'" size="small" class="gap">
          <p v-for="(note, i) in result.notes" :key="i" class="muted">{{ note }}</p>
          <p class="muted">{{ result.usageKnown ? `用量：输入 ${result.inputTokens} / 输出 ${result.outputTokens} Token` : '用量未知，已按预留额度保守计入' }}</p>
          <template v-if="result.plan">
            <n-alert type="info" class="gap">可修改标题与估算时长、调整顺序或移除任务。确认时后端会重新排布时间并检查固定安排与截止时间。</n-alert>
            <n-card v-for="(task, i) in result.plan.tasks" :key="task.id" size="small" class="gap">
              <n-input v-model:value="task.title" :maxlength="160" />
              <n-space align="center" class="gap"><span>估算分钟</span><n-input-number v-model:value="task.minutes" :min="1" :max="480" /><n-button size="small" :disabled="i === 0" @click="move(i, -1)">上移</n-button><n-button size="small" :disabled="i === result.plan.tasks.length - 1" @click="move(i, 1)">下移</n-button><n-button size="small" @click="result.plan.tasks.splice(i, 1)">移除</n-button></n-space>
              <p>{{ task.reason }}</p><ul><li v-for="(step, j) in task.steps" :key="j">{{ step }}</li></ul>
              <n-button text type="primary" @click="selectedSource = sourceFor(task.assignmentId) ?? null">原作业：{{ sourceFor(task.assignmentId)?.title }}</n-button>
            </n-card>
            <n-button type="primary" :disabled="!result.plan.tasks.length || running" @click="confirm">校验并保存学习计划</n-button>
          </template>
          <template v-else><n-empty v-if="!result.sources.length && result.kind === 'query'" description="所选记录中没有匹配项" /><n-card v-for="source in result.sources" :key="source.id" size="small" class="gap"><strong>{{ source.course }} · {{ source.title }}</strong><p>{{ statusNames[source.status] }}{{ source.manual ? '（手动覆盖）' : '' }} · 截止：{{ formatTime(source.deadline) }}</p><n-button text type="primary" @click="selectedSource = source">查看作业依据</n-button></n-card></template>
        </n-card>
      </n-spin>
    </n-card>
  </n-modal>
  <n-modal :show="!!selectedSource" @update:show="value => { if (!value) selectedSource = null }">
    <n-card v-if="selectedSource" title="原作业依据（生成时的本地快照）" class="source-dialog" role="dialog" aria-modal="true">
      <p>{{ platformNames[selectedSource.platform] }} · {{ selectedSource.course }}</p><strong>{{ selectedSource.title }}</strong>
      <p>平台状态：{{ statusNames[selectedSource.platformStatus] }}；有效状态：{{ statusNames[selectedSource.status] }}{{ selectedSource.manual ? '（手动覆盖）' : '' }}</p>
      <p>截止：{{ formatTime(selectedSource.deadline) }}；时间来源：{{ deadlineSources[selectedSource.deadlineSource] ?? '未知' }}</p><p>最后见到：{{ formatTime(selectedSource.lastSeenAt) }}</p>
      <n-button @click="selectedSource = null">关闭</n-button>
    </n-card>
  </n-modal>
</template>

<style scoped>
.ai-dialog { width: 960px; max-width: 94vw; max-height: 92vh; overflow: auto; }
.source-dialog { width: 620px; max-width: 90vw; }
.gap { margin-bottom: 14px; }
.muted { opacity: .75; font-size: 13px; line-height: 1.6; }
.time-grid { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.time-grid input { padding: 8px; border: 1px solid #8b929b; border-radius: 4px; color: inherit; background: transparent; color-scheme: light dark; }
.limits-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; }
.saved-task { padding: 12px 0; border-bottom: 1px solid #8884; }
.preview { margin-top: 14px; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 260px; overflow: auto; font-size: 12px; }
</style>
