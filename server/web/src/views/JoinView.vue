<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ApiError, redeemInvite } from '@/api'
import { useSessionStore } from '@/stores'

const session = useSessionStore()
const router = useRouter()
const route = useRoute()

const token = ref(typeof route.query.token === 'string' ? route.query.token : '')
const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 404) {
      return 'This invite link is no longer valid — it may be expired, already used, or revoked. Ask the household owner for a new one.'
    }
    if (err.status === 409) {
      return 'That username is already taken. Choose a different one.'
    }
    if (err.status === 429) {
      return 'Too many attempts. Wait a bit and try again.'
    }
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await redeemInvite({ token: token.value, username: username.value, password: password.value })
  } catch (err) {
    error.value = describe(err)
    password.value = ''
    busy.value = false
    return
  }

  try {
    await session.logIn(username.value, password.value)
    await router.replace('/items')
  } catch (err) {
    error.value = `Your account was created, but automatic sign-in failed (${describe(err)}). Sign in with your new username and password.`
    await router.push({ name: 'login' })
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="join">
    <h1>Join a household</h1>
    <p class="muted">
      Enter the invite token your household's owner sent you, then choose a username and password.
    </p>

    <form class="card v-stack" @submit.prevent="submit">
      <p v-if="error" class="alert" role="alert">{{ error }}</p>

      <div class="field">
        <label for="token">Invite token</label>
        <input
          id="token"
          v-model="token"
          class="input"
          name="token"
          autocomplete="off"
          required
          :disabled="busy"
        />
      </div>

      <div class="field">
        <label for="username">Username</label>
        <input
          id="username"
          v-model="username"
          class="input"
          name="username"
          autocomplete="username"
          required
          :disabled="busy"
        />
      </div>

      <div class="field">
        <label for="password">Password</label>
        <input
          id="password"
          v-model="password"
          class="input"
          name="password"
          type="password"
          autocomplete="new-password"
          required
          :disabled="busy"
        />
      </div>

      <button
        class="btn btn--primary"
        type="submit"
        :disabled="busy || !token || !username || !password"
      >
        {{ busy ? 'Working…' : 'Join' }}
      </button>
    </form>

    <p class="muted join__note">
      Already have an account? <RouterLink to="/login">Sign in</RouterLink> instead.
    </p>
  </section>
</template>

<style scoped>
.join {
  max-width: 24rem;
  margin: 3rem auto;
  padding: 0 1rem;
}

.join__note {
  font-size: 0.8125rem;
  margin-top: 1.5rem;
}
</style>
