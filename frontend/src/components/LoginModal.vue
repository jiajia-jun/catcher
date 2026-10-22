<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { NAlert, NButton, NModal, NSpace, NSpin, NText, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { useAppStore } from '../stores/app'

const store = useAppStore()
const message = useMessage()

const qrImage = ref('')
const now = ref(Date.now())
let timer: number | undefined

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
    if (show) {
      // 弹窗可能是启动很久后才打开的，先用当前时间刷新，避免倒计时凭空少一截
      tick()
      if (!timer) {
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

async function onRenew() {
  try {
    await store.renewQR()
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
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
    title="雨课堂扫码登录"
    style="width: 380px"
    :mask-closable="false"
    :closable="true"
    @update:show="(value: boolean) => { if (!value) onClose() }"
  >
    <div class="qr-wrap">
      <n-spin :show="!qrImage && !failed" size="large">
        <div class="qr-box">
          <img v-if="qrImage" :src="qrImage" alt="雨课堂登录二维码" class="qr-image" />
          <div v-else class="qr-placeholder">二维码加载中</div>
        </div>
      </n-spin>
      <n-text depth="3" class="expire">{{ remainingText }}</n-text>
      <n-text v-if="store.login.message" class="state-text">{{ store.login.message }}</n-text>
    </div>

    <n-alert v-if="failed" type="warning" :bordered="false" class="alert">
      二维码过期或登录失败时可以换一张；仍未成功请确认手机端雨课堂可正常使用
    </n-alert>

    <n-space justify="space-between" class="actions">
      <n-button quaternary @click="onClose">取消</n-button>
      <n-button type="primary" secondary @click="onRenew">换一张</n-button>
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

.state-text {
  font-size: 13px;
}

.alert {
  margin-top: 12px;
}

.actions {
  margin-top: 18px;
}
</style>
