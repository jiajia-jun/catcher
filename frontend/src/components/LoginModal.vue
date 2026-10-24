<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCheckbox,
  NInput,
  NModal,
  NSpace,
  NSpin,
  NText,
  useMessage,
} from 'naive-ui'
import QRCode from 'qrcode'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const message = useMessage()

const qrImage = ref('')
const now = ref(Date.now())
let timer: number | undefined

const phone = ref('')
const password = ref('')
const remember = ref(true)
const sessionText = ref('')
const formError = ref('')

const isChaoxing = computed(() => store.login.platform === 'chaoxing')
const isTronclass = computed(() => store.login.platform === 'tronclass')
const isYuketang = computed(() => store.login.platform === 'yuketang')
const title = computed(() => {
  if (isTronclass.value) return '学在重邮导入会话'
  return isChaoxing.value ? '学习通账号登录' : '雨课堂扫码登录'
})

function tick() {
  now.value = Date.now()
}

watch(
  () => store.login.qrContent,
  async (content) => {
    tick()
    if (!content) {
      qrImage.value = ''
      return
    }
    try {
      qrImage.value = await QRCode.toDataURL(content, { width: 220, margin: 1 })
    } catch {
      qrImage.value = ''
    }
  },
  { immediate: true },
)

watch(
  () => store.login.show,
  (show) => {
    if (!show) {
      password.value = ''
      sessionText.value = ''
      formError.value = ''
    }
    if (show) {
      // 弹窗可能是启动很久后才打开的，先用当前时间刷新，避免倒计时凭空少一截
      tick()
      // 只有雨课堂二维码需要倒计时
      if (!timer && store.login.platform === 'yuketang') {
        timer = window.setInterval(tick, 1000)
      }
    }
    if (!show && timer) {
      window.clearInterval(timer)
      timer = undefined
    }
  },
)

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

const remaining = computed(() => {
  if (!store.login.expiresAt) return 0
  const end = new Date(store.login.expiresAt).getTime()
  // 向上取整：刚拿到码时显示 5:00，而不是立刻掉到 4:59
  return Math.max(0, Math.ceil((end - now.value) / 1000))
})

const remainingText = computed(() => {
  if (remaining.value <= 0) return '二维码已过期'
  const m = Math.floor(remaining.value / 60)
  const s = remaining.value % 60
  return `${m}:${String(s).padStart(2, '0')} 后过期`
})

const failed = computed(
  () => store.login.state === 'failed' || store.login.state === 'expired',
)

const stateTone = computed(() => {
  if (store.login.state === 'succeeded') return 'is-success'
  if (failed.value) return 'is-error'
  return 'is-neutral'
})

const bannerText = computed(() => formError.value || store.login.message)

async function onRenew() {
  try {
    await store.renewQR()
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  }
}

async function onSubmit() {
  formError.value = ''
  const value = phone.value.trim()
  if (!/^\d{11}$/.test(value)) {
    formError.value = '请输入 11 位手机号'
    return
  }
  if (!password.value) {
    formError.value = '请输入密码'
    return
  }
  try {
    const ok = await store.submitPasswordLogin(value, password.value, remember.value)
    if (ok) password.value = ''
  } catch (err) {
    formError.value = err instanceof Error ? err.message : String(err)
  }
}

async function onSubmitImport() {
  formError.value = ''
  const value = sessionText.value.trim()
  if (!value) {
    formError.value = '请粘贴 session'
    return
  }
  try {
    const ok = await store.submitImportSession(value)
    if (ok) sessionText.value = ''
  } catch (err) {
    formError.value = err instanceof Error ? err.message : String(err)
  }
}

async function onClose() {
  await store.stopLogin()
}
</script>

<template>
  <n-modal
    :show="store.login.show"
    preset="card"
    :title="title"
    style="width: 380px"
    :mask-closable="false"
    :closable="true"
    @update:show="(value: boolean) => { if (!value) onClose() }"
  >
    <div v-if="isTronclass" class="form-wrap">
      <n-input
        v-model:value="sessionText"
        type="password"
        show-password-on="click"
        placeholder="粘贴 session"
        :disabled="store.login.submitting"
        @keyup.enter="onSubmitImport"
      />
      <div v-if="bannerText" class="state-banner" :class="stateTone">
        {{ bannerText }}
      </div>
      <n-text depth="3" class="hint">
        从浏览器开发者工具的 Cookie 中复制 session 值；有效期约 24 小时，失效后需要重新导入
      </n-text>
      <n-alert type="warning" :bordered="false">
        学在重邮接口尚未完整验证，当前只支持导入浏览器 session
      </n-alert>
    </div>

    <div v-else-if="!isChaoxing" class="qr-wrap">
      <n-spin :show="!qrImage && !failed" size="large">
        <div class="qr-box">
          <img v-if="qrImage" :src="qrImage" alt="雨课堂登录二维码" class="qr-image" />
          <div v-else class="qr-placeholder">二维码加载中</div>
        </div>
      </n-spin>
      <n-text depth="3" class="expire">{{ remainingText }}</n-text>
      <div v-if="bannerText" class="state-banner" :class="stateTone">
        {{ bannerText }}
      </div>
    </div>

    <div v-else class="form-wrap">
      <n-input
        v-model:value="phone"
        placeholder="手机号"
        maxlength="11"
        :disabled="store.login.submitting"
        @keyup.enter="onSubmit"
      />
      <n-input
        v-model:value="password"
        type="password"
        show-password-on="click"
        placeholder="密码"
        :disabled="store.login.submitting"
        @keyup.enter="onSubmit"
      />
      <n-checkbox v-model:checked="remember" :disabled="store.login.submitting">
        记住密码，失效时自动重登一次
      </n-checkbox>
      <div v-if="bannerText" class="state-banner" :class="stateTone">
        {{ bannerText }}
      </div>
      <n-text depth="3" class="hint">
        学习通使用手机号 + 密码登录；勾选记住后，密码只在本机用 Windows DPAPI 加密保存
      </n-text>
    </div>

    <n-alert v-if="isYuketang && failed" type="warning" :bordered="false" class="alert">
      二维码过期或登录失败时可以换一张；仍未成功请确认手机端雨课堂可正常使用
    </n-alert>

    <n-space justify="space-between" class="actions">
      <n-button quaternary @click="onClose">取消</n-button>
      <n-button
        v-if="isYuketang"
        type="primary"
        secondary
        @click="onRenew"
      >
        换一张
      </n-button>
      <n-button
        v-else-if="isChaoxing"
        type="primary"
        :loading="store.login.submitting"
        @click="onSubmit"
      >
        登录
      </n-button>
      <n-button
        v-else
        type="primary"
        :loading="store.login.submitting"
        :disabled="!sessionText.trim()"
        @click="onSubmitImport"
      >
        导入
      </n-button>
    </n-space>
  </n-modal>
</template>

<style scoped>
.qr-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 8px 0 4px;
}

.qr-box {
  width: 244px;
  height: 244px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  background: #fff;
}

.qr-image {
  width: 220px;
  height: 220px;
}

.qr-placeholder {
  font-size: 13px;
  opacity: 0.5;
}

.expire {
  font-size: 12px;
}

.form-wrap {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 4px 0;
}

.hint {
  font-size: 12px;
  line-height: 1.6;
}

.state-banner {
  width: 100%;
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 13px;
  text-align: center;
  transition: background-color 0.2s ease, color 0.2s ease;
  box-sizing: border-box;
}

.state-banner.is-success {
  background: #e8f7ee;
  color: #18a058;
}

.state-banner.is-error {
  background: #ffece8;
  color: #d03050;
}

.state-banner.is-neutral {
  background: rgba(128, 128, 128, 0.1);
}

.alert {
  margin-top: 12px;
}

.actions {
  margin-top: 18px;
}
</style>
