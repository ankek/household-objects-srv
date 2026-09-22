import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import {
  ApiError,
  login as loginRequest,
  logout as logoutRequest,
  probeSession,
  register as registerRequest,
} from '@/api'
import type { LoginResponse } from '@/api'

export type SessionStatus = 'unknown' | 'anonymous' | 'authenticated'

export const useSessionStore = defineStore('session', () => {
  const status = ref<SessionStatus>('unknown')
  const user = ref<LoginResponse | null>(null)

  const ready = computed(() => status.value !== 'unknown')
  const isAuthenticated = computed(() => status.value === 'authenticated')
  const isOwner = computed(() => user.value?.role === 'owner')

  async function boot(): Promise<void> {
    if (status.value !== 'unknown') return
    try {
      await probeSession()
      status.value = 'authenticated'
    } catch {
      status.value = 'anonymous'
      user.value = null
    }
  }

  async function logIn(username: string, password: string): Promise<void> {
    const identity = await loginRequest(username, password)
    user.value = identity
    status.value = 'authenticated'
  }

  async function signUp(username: string, password: string): Promise<void> {
    await registerRequest(username, password)
    await logIn(username, password)
  }

  async function logOut(): Promise<void> {
    try {
      await logoutRequest()
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
    } finally {
      user.value = null
      status.value = 'anonymous'
    }
  }

  function expire(): void {
    user.value = null
    status.value = 'anonymous'
  }

  return { status, user, ready, isAuthenticated, isOwner, boot, logIn, signUp, logOut, expire }
})
