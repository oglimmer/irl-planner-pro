import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ActivityLog from './ActivityLog.vue'
import type { ActivityEntry } from '../types'

function makeEntry(overrides: Partial<ActivityEntry> = {}): ActivityEntry {
  return {
    id: '1',
    actorEmail: 'admin@example.com',
    subjectEmail: 'user@example.com',
    action: 'reminder.sent',
    category: 'admin',
    summary: 'Sent weekly reminder',
    afterDeadline: false,
    createdAt: '2026-01-01T12:00:00Z',
    ...overrides,
  }
}

describe('ActivityLog', () => {
  it('renders changes list for notification entries', () => {
    const entry = makeEntry({
      detail: {
        changes: [
          { field: 'IRL team daily summary', from: 'off', to: 'on' },
          { field: 'Admin admin@example.com', from: 'off', to: 'activity (email=true, slack=false)' },
        ],
      },
    })
    const wrapper = mount(ActivityLog, {
      props: { entries: [entry], timezone: 'UTC', showActor: true },
    })
    // The changes list should be present and contain two <li> items.
    const list = wrapper.find('.changes')
    expect(list.exists()).toBe(true)
    const items = list.findAll('li')
    expect(items.length).toBe(2)

    // The generic DL should NOT be rendered because changes is an array.
    expect(wrapper.find('.kv-detail').exists()).toBe(false)
  })

  it('renders generic detail for reminder entries', () => {
    const entry = makeEntry({
      detail: {
        kind: 'weekly',
        period: '2026-W01',
        nonResponders: 5,
        channels: { email: { sent: 5, failed: 0 }, slack: { sent: 0, failed: 0 } },
      },
    })
    const wrapper = mount(ActivityLog, {
      props: { entries: [entry], timezone: 'UTC', showActor: false },
    })
    // The generic DL should be rendered because there is no `changes` array.
    const dl = wrapper.find('.kv-detail')
    expect(dl.exists()).toBe(true)
    // It should display keys as <dt> elements.
    const dts = dl.findAll('dt')
    expect(dts.length).toBeGreaterThanOrEqual(3) // kind, period, nonResponders, channels
    // The changes list should be absent.
    expect(wrapper.find('.changes').exists()).toBe(false)
  })

  it('shows empty state when no entries', () => {
    const wrapper = mount(ActivityLog, {
      props: { entries: [], timezone: 'UTC' },
    })
    expect(wrapper.text()).toContain('No activity yet.')
  })
})
