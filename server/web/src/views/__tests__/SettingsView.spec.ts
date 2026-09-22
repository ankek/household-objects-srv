import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSessionStore } from '@/stores'
import { jsonResponse, stubRoutedFetch, type Routes } from '@/test-support/routedFetch'
import SettingsView from '@/views/SettingsView.vue'

const MEMBERS = 'GET /api/v1/groups/members'
const SESSIONS = 'GET /api/v1/auth/sessions'
const DEVICES = 'GET /api/v1/auth/device-tokens'
const INVITES = 'GET /api/v1/invites'
const CREATE_INVITE = 'POST /api/v1/invites'

function baseRoutes(overrides: Routes = {}): Routes {
  return {
    [MEMBERS]: () => jsonResponse(200, { members: [] }),
    [SESSIONS]: () => jsonResponse(200, { sessions: [] }),
    [DEVICES]: () => jsonResponse(200, { device_tokens: [] }),
    [INVITES]: () => jsonResponse(200, { invites: [] }),
    ...overrides,
  }
}

let pinia: ReturnType<typeof createPinia>

beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function flush(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

async function mountSettings(routes: Routes, role: 'owner' | 'member' = 'owner') {
  const fetchMock = stubRoutedFetch(routes)
  useSessionStore().user = { group_id: 'g', user_id: 'u', username: 'anton', role }
  const wrapper = mount(SettingsView, { global: { plugins: [pinia] } })
  await flush()
  await wrapper.vm.$nextTick()
  return { wrapper, fetchMock }
}

describe('SettingsView placeholders (FR-105 gaps, A115)', () => {
  it('never renders a registration section — FR-105 dropped that sub-screen (A115)', async () => {
    const { wrapper } = await mountSettings(baseRoutes(), 'member')

    expect(wrapper.text()).not.toContain('Registration')
    expect(wrapper.text()).not.toContain('HHO_REGISTRATION_OPEN')
  })
})

describe('SettingsView backup (FR-105, FU-32, owner-only)', () => {
  it('renders no placeholder anywhere on the screen, for either role', async () => {
    const { wrapper: ownerWrapper } = await mountSettings(baseRoutes(), 'owner')
    expect(ownerWrapper.find('.settings__section--placeholder').exists()).toBe(false)

    const { wrapper: memberWrapper } = await mountSettings(baseRoutes(), 'member')
    expect(memberWrapper.find('.settings__section--placeholder').exists()).toBe(false)
  })

  it('gives an owner a download link to the backup endpoint, not a fetch-backed control', async () => {
    const { wrapper, fetchMock } = await mountSettings(baseRoutes(), 'owner')

    const backupCard = wrapper
      .findAll('.settings__section')
      .find((s) => s.get('.settings__section-title').text() === 'Backup and restore')!
    const link = backupCard.get('a')
    expect(link.attributes('href')).toBe('/api/v1/backup')
    expect(link.attributes('download')).toBeDefined()

    expect(fetchMock.mock.calls.some(([input]) => input === '/api/v1/backup')).toBe(false)
  })

  it('renders no Backup section at all for a non-owner, and never links to the endpoint', async () => {
    const { wrapper, fetchMock } = await mountSettings(baseRoutes(), 'member')

    expect(wrapper.text()).not.toContain('Backup and restore')
    expect(wrapper.findAll('a').some((a) => a.attributes('href') === '/api/v1/backup')).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/v1/backup')).toBe(false)
  })
})

describe('SettingsView group members (FR-105, D1.17, member-readable)', () => {
  it('lists members for a plain member too — the route is not owner-gated', async () => {
    const { wrapper } = await mountSettings(
      baseRoutes({
        [MEMBERS]: () =>
          jsonResponse(200, {
            members: [
              { id: 'u1', username: 'anton', role: 'owner', joined_at: 0 },
              { id: 'u2', username: 'jamie', role: 'member', joined_at: 1000 },
            ],
          }),
      }),
      'member',
    )

    const membersCard = wrapper
      .findAll('.settings__section')
      .find((s) => s.get('.settings__section-title').text() === 'Group members')!
    const rows = membersCard.findAll('.settings__row')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.text()).toContain('anton')
    expect(rows[0]!.text()).toContain('owner')
    expect(rows[1]!.text()).toContain('jamie')
    expect(rows[1]!.text()).toContain('member')
    expect(membersCard.find('button').exists()).toBe(false)
  })

  it('shows an empty state when the group has no other members yet', async () => {
    const { wrapper } = await mountSettings(baseRoutes(), 'member')

    const membersCard = wrapper
      .findAll('.settings__section')
      .find((s) => s.get('.settings__section-title').text() === 'Group members')!
    expect(membersCard.text()).toContain('No members.')
  })

  it('reports a load failure without hiding the rest of the screen', async () => {
    const { wrapper } = await mountSettings(
      baseRoutes({
        [MEMBERS]: () => jsonResponse(500, { title: 'Internal Server Error', status: 500 }),
      }),
      'member',
    )

    const membersCard = wrapper
      .findAll('.settings__section')
      .find((s) => s.get('.settings__section-title').text() === 'Group members')!
    expect(membersCard.get('[role="alert"]').text()).toContain('Internal Server Error')
    expect(wrapper.text()).toContain('No sessions.')
  })
})

describe('SettingsView invites (owner-gated, FR-006)', () => {
  it('renders no Invites section at all for a non-owner, and never calls GET /invites', async () => {
    const { wrapper, fetchMock } = await mountSettings(baseRoutes(), 'member')

    expect(wrapper.text()).not.toContain('Invite links let someone join')
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/v1/invites')).toBe(false)
  })

  it('shows the raw token exactly once on creation, and it is gone (but the invite remains listed) after dismissal', async () => {
    const { wrapper } = await mountSettings(
      baseRoutes({
        [CREATE_INVITE]: () =>
          jsonResponse(201, { id: 'inv-1', token: 'raw-token-xyz', expires_at: Date.now() + 1000 }),
      }),
    )
    stubRoutedFetch(
      baseRoutes({
        [CREATE_INVITE]: () =>
          jsonResponse(201, { id: 'inv-1', token: 'raw-token-xyz', expires_at: Date.now() + 1000 }),
        [INVITES]: () =>
          jsonResponse(200, {
            invites: [
              {
                id: 'inv-1',
                created_by_user_id: 'u',
                created_at: Date.now(),
                expires_at: Date.now() + 1000,
                redeemed: false,
              },
            ],
          }),
      }),
    )

    await wrapper.get('.settings__invite-create').trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.get('.settings__invite-code').text()).toBe('raw-token-xyz')

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Dismiss')!
      .trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.settings__invite-code').exists()).toBe(false)
    expect(wrapper.text()).toContain('active')
  })

  it('classifies redeemed, expired, and still-active invites correctly, offering Revoke only for the active one', async () => {
    const now = Date.now()
    const { wrapper } = await mountSettings(
      baseRoutes({
        [INVITES]: () =>
          jsonResponse(200, {
            invites: [
              {
                id: 'redeemed',
                created_by_user_id: 'u',
                created_at: now - 3000,
                expires_at: now + 100000,
                redeemed: true,
                redeemed_at: now - 1000,
              },
              {
                id: 'expired',
                created_by_user_id: 'u',
                created_at: now - 200000,
                expires_at: now - 1000,
                redeemed: false,
              },
              {
                id: 'active',
                created_by_user_id: 'u',
                created_at: now,
                expires_at: now + 100000,
                redeemed: false,
              },
            ],
          }),
      }),
    )

    const rows = wrapper.findAll('.settings__row')
    expect(rows).toHaveLength(3)
    expect(rows[0]!.get('.settings__status').text()).toBe('redeemed')
    expect(rows[1]!.get('.settings__status').text()).toBe('expired')
    expect(rows[2]!.get('.settings__status').text()).toBe('active')

    expect(rows[0]!.find('button').exists()).toBe(false)
    expect(rows[1]!.find('button').exists()).toBe(false)
    expect(rows[2]!.get('button').text()).toBe('Revoke')
  })

  it('revoking an invite removes exactly that row', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => true),
    )
    const { wrapper, fetchMock } = await mountSettings(
      baseRoutes({
        [INVITES]: () =>
          jsonResponse(200, {
            invites: [
              {
                id: 'inv-1',
                created_by_user_id: 'u',
                created_at: Date.now(),
                expires_at: Date.now() + 100000,
                redeemed: false,
              },
            ],
          }),
        ['DELETE /api/v1/invites/inv-1']: () => jsonResponse(204),
      }),
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Revoke')!
      .trigger('click')
    await flush()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.settings__row').exists()).toBe(false)
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) => input === '/api/v1/invites/inv-1' && init?.method === 'DELETE',
      ),
    ).toBe(true)
  })
})

describe('SettingsView sessions and devices (FR-004, self-scoped for either role)', () => {
  it('lists sessions and devices for a plain member too — neither is owner-gated', async () => {
    const { wrapper } = await mountSettings(
      baseRoutes({
        [SESSIONS]: () =>
          jsonResponse(200, {
            sessions: [
              {
                id: 's1',
                user_agent: 'Firefox',
                created_from_ip: '10.0.0.1',
                created_at: 0,
                expires_at: 0,
                revoked: false,
              },
            ],
          }),
        [DEVICES]: () =>
          jsonResponse(200, {
            device_tokens: [{ id: 'd1', device_label: 'Pixel', created_at: 0, revoked: false }],
          }),
      }),
      'member',
    )

    expect(wrapper.text()).toContain('Firefox')
    expect(wrapper.text()).toContain('Pixel')
  })

  it('asks for confirmation before signing out a session, and does nothing if declined', async () => {
    vi.stubGlobal(
      'confirm',
      vi.fn(() => false),
    )
    const { wrapper, fetchMock } = await mountSettings(
      baseRoutes({
        [SESSIONS]: () =>
          jsonResponse(200, {
            sessions: [
              {
                id: 's1',
                user_agent: 'Firefox',
                created_from_ip: '10.0.0.1',
                created_at: 0,
                expires_at: 0,
                revoked: false,
              },
            ],
          }),
      }),
      'member',
    )
    const callsAtMount = fetchMock.mock.calls.length

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Sign out')!
      .trigger('click')
    await flush()

    expect(fetchMock.mock.calls.length).toBe(callsAtMount)
    expect(wrapper.text()).toContain('Firefox')
  })

  it('a failed session load reports the error without hiding the rest of the screen', async () => {
    const { wrapper } = await mountSettings(
      baseRoutes({
        [SESSIONS]: () => jsonResponse(500, { title: 'Internal Server Error', status: 500 }),
      }),
      'member',
    )

    const sessionsCard = wrapper
      .findAll('.settings__section')
      .find((s) => s.get('.settings__section-title').text() === 'Sessions')!
    expect(sessionsCard.get('[role="alert"]').text()).toContain('Internal Server Error')
    expect(wrapper.text()).toContain('No registered devices.')
  })
})
