const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const vue = require('vue')
const ts = require('typescript')
const { parse, compileScript } = require('@vue/compiler-sfc')

const source = fs.readFileSync(path.join(__dirname, '../src/components/AIStudyModal.vue'), 'utf8')
const compiled = compileScript(parse(source).descriptor, { id: 'ai-regression' }).content
const js = ts.transpileModule(compiled, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const config = { enabled: false, baseUrl: 'https://example.invalid', model: 'mock', maxInputChars: 12000, maxOutputTokens: 2048, monthlyTokenLimit: 200000, timeoutSeconds: 60 }
const assignment = { id: 1, platform: 'chaoxing', courseName: '数学', title: '数学作业', status: 'unsubmitted' }
const sourceFact = { id: 1, ref: 'A1', platform: 'chaoxing', course: '数学', title: '数学作业', status: 'unsubmitted' }

function fixture(t, overrides = {}) {
  const calls = [], cleanup = [], module = { exports: {} }
  const responses = {
    GetAISettings: { settings: { ...config }, hasKey: false, usage: { month: '2026-10', chargedTokens: 0, reservedTokens: 0, runs: 0 } },
    ListStudyPlans: { plans: [] },
    PreviewAI: { snapshot: 'preview-token', sources: [sourceFact], coverage: [], payload: '{"question":"数学优先"}', system: 'json', endpoint: config.baseUrl, model: config.model },
    StartAI: { jobId: 'job-1' },
    GetAIJob: { id: 'job-1', state: 'succeeded', kind: 'query', sources: [sourceFact], coverage: [], notes: [], usageKnown: true, inputTokens: 12, outputTokens: 8 },
    SaveAISettings: { settings: { ...config, enabled: true }, hasKey: true, usage: { month: '2026-10', chargedTokens: 0, reservedTokens: 0, runs: 0 } },
    CancelAI: {}, ConfirmStudyPlan: {}, SetStudyTaskDone: {}, ...overrides,
  }
  vm.runInNewContext(js, {
    exports: module.exports, module,
    require: name => name === 'vue' ? { ...vue, onBeforeUnmount: fn => cleanup.push(fn) }
      : name === 'naive-ui' ? new Proxy({}, { get: () => ({}) })
      : name === '../api/ai' ? { aiCall: async (name, ...args) => {
        calls.push({ name, args: JSON.parse(JSON.stringify(args)) })
        const value = responses[name]
        if (value instanceof Error) throw value
        return structuredClone(value ?? {})
      } }
      : name === '../api/app' ? { loadAssignments: async () => ({ items: [assignment] }) }
      : require(name),
    setTimeout, clearTimeout, console,
  })
  const props = vue.reactive({ show: false })
  const scope = vue.effectScope()
  const state = scope.run(() => module.exports.default.setup(props, { expose() {}, emit() {} }))
  t.after(() => { cleanup.forEach(fn => fn()); scope.stop() })
  return { state, props, calls, responses }
}

test('打开助手只读取本地状态，不自动启动云模型', async t => {
  const h = fixture(t)
  await h.state.initialize()
  assert.equal(h.state.settings.value.enabled, false)
  assert.equal(h.state.selected.value.length, 0)
  assert.equal(h.calls.filter(c => c.name === 'StartAI').length, 0)
})

test('没有预览或未勾选同意时不发送', async t => {
  const h = fixture(t)
  await h.state.send()
  h.state.preview.value = h.responses.PreviewAI
  await h.state.send()
  assert.equal(h.calls.filter(c => c.name === 'StartAI').length, 0)
})

for (const field of ['question', 'selected', 'starts', 'blocked']) {
  test(`修改 ${field} 后失效预览与同意状态`, async t => {
    const h = fixture(t)
    h.state.preview.value = h.responses.PreviewAI
    h.state.consent.value = true
    h.state.result.value = { ...h.responses.GetAIJob, plan: { title: '旧条件的草案' } }
    h.state[field].value = field === 'selected' ? [1] : field === 'blocked' ? [{ start: '2026-11-01T09:00', end: '2026-11-01T10:00' }] : 'changed'
    await vue.nextTick()
    assert.equal(h.state.preview.value, null)
    assert.equal(h.state.consent.value, false)
    assert.equal(h.state.result.value, null)
  })
}

test('确认发送携带相同选择与预览令牌，结果使用后端事实卡片', async t => {
  const h = fixture(t)
  await h.state.initialize()
  h.state.kind.value = 'query'; h.state.question.value = '数学优先'; h.state.selected.value = [1]
  await vue.nextTick()
  await h.state.makePreview()
  h.state.consent.value = true
  await h.state.send()
  await new Promise(resolve => setImmediate(resolve))
  const sent = h.calls.find(c => c.name === 'StartAI').args[0]
  assert.equal(sent.snapshot, 'preview-token')
  assert.deepEqual(sent.assignmentIds, [1])
  assert.equal(h.state.result.value.sources[0].id, 1)
  assert.equal(h.state.running.value, false)
})

test('保存设置后清空输入框 Key；关闭窗口也会清除未提交 Key', async t => {
  const h = fixture(t)
  h.state.apiKey.value = 'FAKE_SECRET'
  await h.state.saveSettings()
  assert.equal(h.calls.find(c => c.name === 'SaveAISettings').args[0].apiKey, 'FAKE_SECRET')
  assert.equal(h.state.apiKey.value, '')
  h.props.show = true; await vue.nextTick()
  h.state.apiKey.value = 'ANOTHER_FAKE_SECRET'
  h.props.show = false; await vue.nextTick()
  assert.equal(h.state.apiKey.value, '')
})

test('保存校验失败时保留可编辑草案', async t => {
  const h = fixture(t, { ConfirmStudyPlan: new Error('作业已变化') })
  h.state.result.value = { ...h.responses.GetAIJob, plan: { tasks: [{ id: 't', assignmentId: 1, minutes: 30 }], title: '草案' } }
  await h.state.confirm()
  assert.match(h.state.error.value, /作业已变化$/)
  assert.equal(h.state.result.value.plan.title, '草案')
})

test('本地子任务完成调用独立接口，不修改平台作业状态', async t => {
  const h = fixture(t)
  await h.state.setDone({ id: 'p' }, 't', true)
  assert.equal(h.calls[0].name, 'SetStudyTaskDone')
  assert.deepEqual(h.calls[0].args[0], { planId: 'p', taskId: 't', done: true })
  assert(!h.calls.some(c => c.name === 'SetAssignmentStatus'))
})
