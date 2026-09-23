<script setup lang="ts">
import { RouterLink, useRouter } from 'vue-router'
import { useSessionStore } from '@/stores'

const session = useSessionStore()
const router = useRouter()

async function signOut(): Promise<void> {
  await session.logOut()
  await router.replace('/login')
}
</script>

<template>
  <header class="app-nav">
    <RouterLink class="app-nav__brand" to="/items">HHO</RouterLink>

    <nav v-if="session.isAuthenticated" class="app-nav__links">
      <RouterLink to="/items">Items</RouterLink>
      <RouterLink to="/locations">Locations</RouterLink>
      <RouterLink to="/labels">Labels</RouterLink>
      <RouterLink to="/print-labels">Print labels</RouterLink>
      <RouterLink to="/import">Import</RouterLink>
      <RouterLink to="/reports">Reports</RouterLink>
      <RouterLink to="/settings">Settings</RouterLink>
    </nav>

    <span class="app-nav__spacer"></span>

    <div v-if="session.isAuthenticated" class="h-stack">
      <span v-if="session.user" class="muted app-nav__user" :title="session.user.username">{{
        session.user.username
      }}</span>
      <button class="btn btn--ghost btn--small" type="button" @click="signOut">Sign out</button>
    </div>
  </header>
</template>

<style scoped>
.app-nav {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  padding: 0.75rem 1.5rem;
  border-bottom: 1px solid var(--hho-border);
}

.app-nav__brand {
  font-weight: 700;
  letter-spacing: 0.04em;
  text-decoration: none;
  color: inherit;
}

.app-nav__links {
  display: flex;
  gap: 1rem;
}

.app-nav__links a {
  text-decoration: none;
  color: var(--hho-muted);
  padding: 0.15rem 0;
  border-bottom: 2px solid transparent;
}

.app-nav__links a:hover {
  color: var(--hho-fg);
}

.app-nav__links a.router-link-active {
  color: var(--hho-fg);
  border-bottom-color: var(--hho-accent);
}

.app-nav__spacer {
  flex: 1;
}

.app-nav__user {
  font-size: 0.875rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 40vw;
}

@media (max-width: 40rem) {
  .app-nav {
    flex-wrap: wrap;
    row-gap: 0.5rem;
  }

  .app-nav__spacer {
    display: none;
  }

  .app-nav__links {
    width: 100%;
    flex-wrap: wrap;
  }

  .app-nav > .h-stack {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>
