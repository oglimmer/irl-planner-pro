import type { AttendingState } from '../types'

// Human labels for each RSVP state, shared by the dashboard's tabs.
export const attendingLabels: Record<AttendingState, string> = {
  yes: 'Yes',
  no: 'No',
  not_sure: 'Not sure',
  no_response: 'No response',
}

// Sort weight for attendance: a logical pipeline order (going → maybe → no →
// silent) rather than alphabetical, so a descending sort surfaces the most
// notable states first.
export const attendingRank: Record<AttendingState, number> = {
  yes: 0,
  not_sure: 1,
  no: 2,
  no_response: 3,
}

// Case-insensitive "contains" across several optional fields — the client-side
// search predicate shared by the responses / attendees / activity tabs. `q` is
// expected pre-lowercased and trimmed by the caller.
export function matchesQuery(q: string, ...fields: (string | undefined)[]): boolean {
  return fields.some((f) => (f ?? '').toLowerCase().includes(q))
}
