import { ErrNotFound } from './types'

export type MonitorKind = 'http' | 'tcp' | 'disciple' | 'heartbeat'

export const MONITOR_KIND_HTTP: MonitorKind = 'http'
export const MONITOR_KIND_TCP: MonitorKind = 'tcp'
export const MONITOR_KIND_DISCIPLE: MonitorKind = 'disciple'
export const MONITOR_KIND_HEARTBEAT: MonitorKind = 'heartbeat'
export const MONITOR_NAME_HEARTBEAT = 'heartbeat'

export interface MonitorCheckDay {
  monitor_id: string
  app_id: string
  day: string
  ok_count: number
  failed_count: number
}

export interface Monitor {
  id: string
  app_id: string
  name: string
  kind: MonitorKind
  target?: string
  interval_seconds: number
  timeout_seconds: number
  expected_status: number
  enabled: boolean
  owner_id: string
  created_by: string
  deleted_at?: string
  created_at: string
  updated_at: string
  last_checked_at?: string
  last_ok?: boolean
  app_slug?: string
  app_name?: string
  days?: MonitorCheckDay[]
}

export interface MonitorCheck {
  id: string
  app_id: string
  monitor_id: string
  checked_at: string
  ok: boolean
  status_code?: number
  latency_ms: number
  error?: string
  owner_id: string
}

export interface MonitorIncident {
  id: string
  app_id: string
  monitor_id: string
  opened_at: string
  resolved_at?: string
  title: string
  description?: string
  owner_id: string
  created_by: string
  deleted_at?: string
  created_at: string
  updated_at: string
  app_slug?: string
  app_name?: string
  monitor_name?: string
}

export interface MonitorStatus {
  app_slug: string
  app_name: string
  monitors: Monitor[]
  incidents: MonitorIncident[]
}

export interface BeatCheck {
  name: string
  ok: boolean
  error?: string
}

export interface Beat {
  ok: boolean
  latency_ms: number
  version: string
  checks: BeatCheck[]
}

export interface Heartbeat extends Beat {
  monitor_id: string
  beat_at: string
  reported: boolean
}

export function appStatusUrl(endpoint: string, slug: string): string {
  return `${endpoint.replace(/\/$/, '')}/api/app/${encodeURIComponent(slug)}/status`
}

export async function getAppStatus(endpoint: string, slug: string): Promise<MonitorStatus> {
  const res = await fetch(appStatusUrl(endpoint, slug), { headers: { Accept: 'application/json' } })
  if (res.status === 404) throw ErrNotFound
  if (!res.ok) throw new Error(`status returned ${res.status}`)
  const body = (await res.json()) as { data?: MonitorStatus }
  if (!body.data) throw new Error('status returned no data')
  return body.data
}
