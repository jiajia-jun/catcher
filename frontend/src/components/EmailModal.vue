<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputGroup,
  NInputGroupLabel,
  NModal,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
  useMessage,
} from 'naive-ui'
import { loadEmailSettings, saveEmailSettings, sendTestEmail } from '../api/app'
import { enqueueToast } from '../composables/toasts'
import { useAppStore } from '../stores/app'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', value: boolean): void }>()

const store = useAppStore()
const message = useMessage()

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

interface ProviderPreset {
  key: string
  label: string
  smtp: string
  hint: string
}

const providers: ProviderPreset[] = [
  {
    key: 'qq',
    label: 'QQ 邮箱',
    smtp: 'smtp.qq.com',
    hint: 'QQ 邮箱网页版 → 设置 → 账户 → 开启「POP3/SMTP 服务」，按提示生成授权码',
  },
  {
    key: '163',
    label: '网易 163',
    smtp: 'smtp.163.com',
    hint: '163 邮箱 → 设置 → POP3/SMTP/IMAP → 开启 SMTP 服务后获取授权码',
  },
  {
    key: '126',
    label: '网易 126',
    smtp: 'smtp.126.com',
    hint: '126 邮箱 → 设置 → POP3/SMTP/IMAP → 开启 SMTP 服务后获取授权码',
  },
  {
    key: 'gmail',
    label: 'Gmail',
    smtp: 'smtp.gmail.com',
    hint: 'Gmail 需先开启两步验证，再创建「应用专用密码」作为授权码',
  },
  {
    key: 'custom',
    label: '自定义 SMTP',
    smtp: '',
    hint: '填写邮件服务商提供的 SMTP 服务器地址；端口固定 465（SSL）',
  },
]

const providerOptions = providers.map((item) => ({
  label: item.label,
  value: item.key,
}))

const form = reactive({
  provider: 'qq',
  address: '',
  smtp: 'smtp.qq.com',
  password: '',
})

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const configured = ref(false)
const savedPassword = ref(false)
const submitted = ref(false)
const port = ref(465)

const currentProvider = computed(
  () => providers.find((item) => item.key === form.provider) ?? providers[providers.length - 1],
)

const emailValid = computed(() => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.address.trim()))
const smtpValid = computed(() => {
  const host = form.smtp.trim()
  return host.length > 0 && !/\s/.test(host)
})
const passwordValid = computed(() => form.password.trim().length > 0 || savedPassword.value)

const addressFeedback = computed(() =>
  submitted.value && !emailValid.value ? '请输入有效的邮箱地址' : '',
)
const smtpFeedback = computed(() =>
  submitted.value && !smtpValid.value ? '请输入 SMTP 服务器地址' : '',
)
const passwordFeedback = computed(() =>
  submitted.value && !passwordValid.value ? '首次配置需要填写授权码' : '',
)

const testSubject = 'Catcher 邮件配置测试'

// 预览与真正发送使用同一份正文，避免两边对不上
const previewBody = computed(() => {
  const lines = [
    '这是一封来自 Catcher 的测试邮件。',
    '收到它，说明邮箱配置可以正常发信。',
  ]
  const stats = store.bootstrap?.stats
  if (stats) {
    lines.push(
      '',
      `当前待提交 ${stats.openUnsubmitted} 条 · 已截止未交 ${stats.overdueUnsubmitted} 条 · 状态未知 ${stats.unknown} 条`,
    )
  }
  lines.push('', '—— Catcher')
  return lines.join('\n')
})

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function providerFor(smtp: string): string {
  const host = smtp.trim().toLowerCase()
  if (!host) return 'qq'
  return (
    providers.find((item) => item.key !== 'custom' && item.smtp === host)?.key ?? 'custom'
  )
}

function onProviderChange(value: string | number) {
  const key = String(value)
  form.provider = key
  if (key === 'custom') return
  const preset = providers.find((item) => item.key === key)
  if (preset) form.smtp = preset.smtp
}

function onSmtpChange(value: string) {
  form.smtp = value
  const host = value.trim().toLowerCase()
  form.provider =
    providers.find((item) => item.key !== 'custom' && item.smtp === host)?.key ?? 'custom'
}

// 接受从服务商文档里粘贴的完整地址，端口由后端固定为 465
function normalizeSmtp(raw: string): string {
  const host = raw
    .trim()
    .replace(/^smtps?:\/\//i, '')
    .replace(/[/?#].*$/, '')
  return host.replace(/:\d+$/, '').trim()
}

watch(
  () => props.show,
  async (show) => {
    if (!show) return
    submitted.value = false
    form.password = ''
    loading.value = true
    try {
      const settings = await loadEmailSettings()
      configured.value = settings.configured
      savedPassword.value = settings.configured
      form.address = settings.address ?? ''
      // 还没配置过时给一个可编辑的默认值，而不是让服务商与空 SMTP 对不上
      form.smtp = settings.smtp || providers[0].smtp
      port.value = settings.port || 465
      form.provider = providerFor(form.smtp)
    } catch (err) {
      enqueueToast({
        tone: 'error',
        title: '读取邮箱配置失败',
        content: errorText(err),
        duration: 4000,
      })
    } finally {
      loading.value = false
    }
  },
)

async function persist(notify: boolean): Promise<boolean> {
  submitted.value = true
  if (!emailValid.value || !smtpValid.value || !passwordValid.value) {
    message.warning('请先补全邮箱配置')
    return false
  }
  saving.value = true
  try {
    const settings = await saveEmailSettings({
      address: form.address.trim(),
      smtp: normalizeSmtp(form.smtp),
      password: form.password,
    })
    configured.value = settings.configured
    savedPassword.value = settings.configured
    form.address = settings.address ?? form.address
    form.smtp = settings.smtp ?? form.smtp
    port.value = settings.port || port.value
    form.provider = providerFor(form.smtp)
    form.password = ''
    if (notify) {
      enqueueToast({
        title: '邮箱配置已保存',
        content: form.address,
      })
    }
    return true
  } catch (err) {
    enqueueToast({
      tone: 'error',
      title: '保存邮箱配置失败',
      content: errorText(err),
      duration: 4000,
    })
    return false
  } finally {
    saving.value = false
  }
}

function onSave() {
  if (loading.value || testing.value) return
  void persist(true)
}

async function onTest() {
  if (loading.value || saving.value || testing.value) return
  // 测试使用当前表单内容：先保存，再按保存后的配置发送
  if (!(await persist(false))) return
  testing.value = true
  try {
    await sendTestEmail({ topic: testSubject, desc: previewBody.value })
    enqueueToast({
      title: '测试邮件已发送',
      content: form.address ? `已发送到 ${form.address}` : '已发送到发件邮箱',
    })
  } catch (err) {
    enqueueToast({
      tone: 'error',
      title: '测试邮件未发出',
      content: errorText(err),
      duration: 5000,
    })
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <n-modal
    v-model:show="visible"
    preset="card"
    title="邮件通知"
    size="huge"
    :style="{ width: '860px', maxWidth: '94vw' }"
    :mask-closable="false"
  >
    <template #header-extra>
      <n-tag size="small" :type="configured ? 'success' : 'default'" :bordered="false">
        {{ configured ? '已配置' : '未配置' }}
      </n-tag>
    </template>

    <n-spin :show="loading">
      <div class="email-grid">
        <section class="pane">
          <div class="pane-head">
            <h3>邮件服务</h3>
            <p>用你的邮箱给自己发送测试邮件</p>
          </div>

          <n-form label-placement="top" :show-require-mark="false" class="email-form">
            <n-form-item label="服务商">
              <n-select
                :value="form.provider"
                :options="providerOptions"
                :disabled="saving || testing"
                @update:value="onProviderChange"
              />
            </n-form-item>

            <n-form-item
              label="发件邮箱"
              :validation-status="addressFeedback ? 'error' : undefined"
              :feedback="addressFeedback"
            >
              <n-input
                v-model:value="form.address"
                placeholder="name@example.com"
                :disabled="saving || testing"
              />
            </n-form-item>

            <n-form-item
              label="授权码"
              :validation-status="passwordFeedback ? 'error' : undefined"
              :feedback="passwordFeedback"
            >
              <n-input
                v-model:value="form.password"
                type="password"
                show-password-on="click"
                :placeholder="savedPassword ? '留空保持已保存的授权码' : '邮箱服务商提供的授权码'"
                :disabled="saving || testing"
              />
            </n-form-item>

            <n-form-item
              label="SMTP 服务器"
              :validation-status="smtpFeedback ? 'error' : undefined"
              :feedback="smtpFeedback"
            >
              <n-input-group>
                <n-input
                  :value="form.smtp"
                  placeholder="smtp.example.com"
                  :disabled="saving || testing"
                  @update:value="onSmtpChange"
                />
                <n-input-group-label>:{{ port }}</n-input-group-label>
              </n-input-group>
            </n-form-item>
          </n-form>

          <n-alert type="info" :bordered="false" class="auth-hint">
            {{ currentProvider.hint }}。这里的授权码不是邮箱登录密码。
          </n-alert>
        </section>

        <section class="pane preview-pane">
          <div class="pane-head">
            <h3>邮件预览</h3>
            <p>发送前会先保存当前配置</p>
          </div>

          <div class="preview-card">
            <div class="preview-meta">
              <div class="meta-row">
                <span>发件</span>
                <strong>{{ form.address || '尚未填写' }}</strong>
              </div>
              <div class="meta-row">
                <span>收件</span>
                <strong>{{ form.address || '尚未填写' }}</strong>
              </div>
              <div class="meta-row">
                <span>主题</span>
                <strong>{{ testSubject }}</strong>
              </div>
            </div>
            <div class="preview-body">{{ previewBody }}</div>
          </div>

          <n-space vertical :size="8" class="preview-actions">
            <n-button
              type="primary"
              secondary
              block
              :loading="testing"
              :disabled="loading || saving"
              @click="onTest"
            >
              发送测试邮件
            </n-button>
            <n-text depth="3" class="preview-tip">
              测试邮件只会发送到上面的发件邮箱
            </n-text>
          </n-space>
        </section>
      </div>
    </n-spin>

    <template #footer>
      <n-space justify="space-between" align="center">
        <n-text depth="3" class="footer-note">授权码经 DPAPI 加密后保存在本机</n-text>
        <n-space :size="8">
          <n-button quaternary @click="visible = false">取消</n-button>
          <n-button
            type="primary"
            :loading="saving"
            :disabled="loading || testing"
            @click="onSave"
          >
            保存
          </n-button>
        </n-space>
      </n-space>
    </template>
  </n-modal>
</template>

<style scoped>
.email-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 0.95fr);
  gap: 26px;
}

.pane {
  min-width: 0;
}

.pane-head h3 {
  margin: 0 0 2px;
  font-size: 15px;
}

.pane-head p {
  margin: 0 0 14px;
  font-size: 12px;
  opacity: 0.6;
}

.auth-hint {
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.6;
}

.preview-pane {
  display: flex;
  flex-direction: column;
}

.preview-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(128, 128, 128, 0.24);
  border-radius: 12px;
  overflow: hidden;
  background: rgba(128, 128, 128, 0.04);
}

.preview-meta {
  display: grid;
  gap: 6px;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(128, 128, 128, 0.18);
  background: rgba(128, 128, 128, 0.06);
}

.meta-row {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr);
  gap: 8px;
  align-items: baseline;
  font-size: 12px;
}

.meta-row span {
  opacity: 0.55;
}

.meta-row strong {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-body {
  padding: 16px;
  font-size: 13px;
  line-height: 1.75;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  min-height: 190px;
}

.preview-actions {
  margin-top: 14px;
}

.preview-tip {
  display: block;
  font-size: 12px;
  text-align: center;
}

.footer-note {
  font-size: 12px;
}

@media (max-width: 760px) {
  .email-grid {
    grid-template-columns: 1fr;
  }
}
</style>
