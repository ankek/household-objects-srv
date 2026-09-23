<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api'
import { useSessionStore } from '@/stores'

const session = useSessionStore()
const router = useRouter()
const route = useRoute()

const mode = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    return err.problem?.detail
      ? `${err.problem.title}: ${err.problem.detail}`
      : (err.problem?.title ?? err.message)
  }
  return 'Could not reach the server. Check that it is running and try again.'
}

function switchMode(next: 'login' | 'register'): void {
  mode.value = next
  error.value = ''
}

function handleRegisterConflict(): void {
  error.value = 'This server already has an account. Sign in with it instead.'
  mode.value = 'login'
}

async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    if (mode.value === 'register') {
      await session.signUp(username.value, password.value)
    } else {
      await session.logIn(username.value, password.value)
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/items'
    await router.replace(redirect)
  } catch (err) {
    if (mode.value === 'register' && err instanceof ApiError && err.status === 409) {
      handleRegisterConflict()
    } else {
      error.value = describe(err)
    }
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="login">
    <h1>HHO</h1>
    <p class="muted">Household inventory. Everything lives on your own server.</p>

    <form class="card v-stack" @submit.prevent="submit">
      <div class="login__modes" role="group" aria-label="Sign in or create the first account">
        <button
          type="button"
          class="login__mode"
          :class="{ 'login__mode--on': mode === 'login' }"
          :aria-pressed="mode === 'login'"
          @click="switchMode('login')"
        >
          Sign in
        </button>
        <button
          type="button"
          class="login__mode"
          :class="{ 'login__mode--on': mode === 'register' }"
          :aria-pressed="mode === 'register'"
          @click="switchMode('register')"
        >
          Create the first account
        </button>
      </div>

      <p v-if="mode === 'register'" class="muted login__explain">
        Only works on a server that has no account yet. This creates the household and makes you its
        owner.
      </p>

      <p v-if="error" class="alert" role="alert">{{ error }}</p>

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
          :autocomplete="mode === 'register' ? 'new-password' : 'current-password'"
          required
          :disabled="busy"
        />
      </div>

      <button class="btn btn--primary" type="submit" :disabled="busy || !username || !password">
        {{ busy ? 'Working…' : mode === 'register' ? 'Create account' : 'Sign in' }}
      </button>
    </form>

    <p class="muted login__join">
      Have an invite? <RouterLink to="/join">Join a household</RouterLink>.
    </p>

    <p class="muted login__note">
      Reaching this over plain HTTP from another machine? Sign-in needs either
      <code>localhost</code> or HTTPS — the session cookie is marked <code>Secure</code>, and a
      browser will not keep it on an unencrypted connection to a remote host.
    </p>
  </section>
</template>

<style scoped>
.login {
  max-width: 24rem;
  margin: 3rem auto;
  padding: 0 1rem;
}

.login__heading {
  margin: 0;
  font-size: 1.125rem;
}

.login__modes {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.25rem;
  padding: 0.25rem;
  background: var(--hho-bg);
  border: 1px solid var(--hho-border);
  border-radius: var(--hho-radius);
}

.login__mode {
  font: inherit;
  font-size: 0.875rem;
  color: var(--hho-muted);
  background: none;
  border: none;
  border-radius: calc(var(--hho-radius) - 2px);
  padding: 0.4rem 0.5rem;
  cursor: pointer;
}

.login__mode:hover {
  color: var(--hho-fg);
}

.login__mode--on {
  background: var(--hho-accent);
  color: var(--hho-accent-fg);
  font-weight: 600;
}

.login__explain {
  margin: 0;
  font-size: 0.8125rem;
}

.login__join {
  font-size: 0.8125rem;
  margin-top: 1.5rem;
}

.login__note {
  font-size: 0.8125rem;
  margin-top: 1.5rem;
}

code {
  background: var(--hho-surface);
  padding: 0.1em 0.35em;
  border-radius: 4px;
}
</style>
