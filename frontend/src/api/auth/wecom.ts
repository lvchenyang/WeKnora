import { get, post, put } from '@/utils/request'
import type { LoginResponse } from './index'

export interface WeComBinding {
  id: string; corp_id: string; subject: string; user_id: string; display_name: string
  status: 'active' | 'suspended' | 'revoked'; version: number
}
export interface WeComPreview {
  preview_token: string; user_id: string; email: string; username: string
  corp_id: string; subject: string; display_name: string
}
export interface WeComEvent { id: string; actor_id: string; action: string; user_id: string; version: number; created_at: string }
const admin = '/api/v1/system/admin/wecom'

// Public login requests deliberately bypass authenticated Axios refresh logic.
// A failed exchange must not rotate a pre-existing account's session.
export async function getWeComConfig(): Promise<{ enabled: boolean }> {
  const response = await fetch('/api/v1/auth/wecom/config', { cache: 'no-store' })
  if (!response.ok) throw new Error('wecom_config_failed')
  return response.json()
}
export async function exchangeWeComLogin(flowId: string): Promise<LoginResponse> {
  const response = await fetch('/api/v1/auth/wecom/exchange', {
    method: 'POST', credentials: 'same-origin', cache: 'no-store',
    headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ flow_id: flowId }),
  })
  if (!response.ok) throw new Error('wecom_failed')
  return response.json()
}
export const listWeComBindings = (offset = 0) => get<{ bindings: WeComBinding[]; users: Record<string, { email: string; username: string }>; corp_id: string }>(`${admin}/bindings?offset=${offset}`)
export const previewWeComBinding = (email: string, subject: string) => post<WeComPreview>(`${admin}/preview`, { email, subject })
export const bindWeComIdentity = (preview_token: string) => post<WeComBinding>(`${admin}/bindings`, { preview_token })
export const setWeComBindingStatus = (binding: WeComBinding, status: WeComBinding['status']) => put(`${admin}/bindings/${binding.id}`, { status, version: binding.version })
export const getWeComBindingEvents = (id: string) => get<{ events: WeComEvent[] }>(`${admin}/bindings/${id}/events`)
