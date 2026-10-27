import { ref } from 'vue'

export type ToastTone = 'success' | 'error'

export interface ToastAction {
  label: string
  run: () => void
}

export interface ToastItem {
  id: number
  title: string
  content: string
  duration: number
  tone: ToastTone
  assignmentId?: number
  action?: ToastAction
}

export interface ToastInput {
  title: string
  content: string
  duration?: number
  tone?: ToastTone
  assignmentId?: number
  action?: ToastAction
}

interface ToastTimer {
  remaining: number
  startedAt: number
  handle: number
}

// 普通提示的停留时长；错误提示由调用方传入更长的时长
const defaultDuration = 2000

// 模块级状态：主界面渲染提示栈，任意子组件都可以投递
export const toasts = ref<ToastItem[]>([])
const toastTimers = new Map<number, ToastTimer>()
let toastSeq = 0

export function enqueueToast(input: ToastInput) {
  const id = ++toastSeq
  // 同一行只保留最近一次可撤销的操作
  if (input.assignmentId !== undefined) {
    dismissToastsFor(input.assignmentId)
  }
  const duration = input.duration ?? defaultDuration
  toasts.value.push({
    id,
    title: input.title,
    content: input.content,
    duration,
    tone: input.tone ?? 'success',
    assignmentId: input.assignmentId,
    action: input.action,
  })
  toastTimers.set(id, { remaining: duration, startedAt: 0, handle: 0 })
  scheduleToast(id)
}

function scheduleToast(id: number) {
  const state = toastTimers.get(id)
  if (!state) return
  state.startedAt = Date.now()
  state.handle = window.setTimeout(() => dismissToast(id), state.remaining)
}

export function pauseToast(id: number) {
  const state = toastTimers.get(id)
  if (!state) return
  window.clearTimeout(state.handle)
  state.remaining = Math.max(0, state.remaining - (Date.now() - state.startedAt))
  if (state.remaining === 0) dismissToast(id)
}

export function resumeToast(id: number) {
  const state = toastTimers.get(id)
  if (!state || state.remaining <= 0) return
  scheduleToast(id)
}

export function dismissToast(id: number) {
  const state = toastTimers.get(id)
  if (state) window.clearTimeout(state.handle)
  toastTimers.delete(id)
  toasts.value = toasts.value.filter((item) => item.id !== id)
}

function dismissToastsFor(assignmentId: number) {
  for (const item of [...toasts.value]) {
    if (item.assignmentId === assignmentId) dismissToast(item.id)
  }
}

export function runToastAction(item: ToastItem) {
  const action = item.action
  dismissToast(item.id)
  action?.run()
}

export function clearToastTimers() {
  for (const state of toastTimers.values()) window.clearTimeout(state.handle)
  toastTimers.clear()
}
