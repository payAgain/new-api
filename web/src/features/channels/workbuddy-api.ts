/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type WorkBuddyState = {
  accounts: {
    index: number
    nickname: string
    region: string
    enabled: boolean
    expiresAtMs: number
    checkinDay: string
  }[]
  distribution: string
  autoCheckin: boolean
}
export async function getWorkBuddyState(
  id: number,
  signal?: AbortSignal
): Promise<WorkBuddyState> {
  const response = await api.get<{ success: boolean; data: WorkBuddyState }>(
    `/api/channel/${id}/workbuddy`,
    { signal }
  )
  return requireServerSuccess(response.data).data
}
export async function updateWorkBuddySettings(
  id: number,
  settings: {
    autoCheckin?: boolean
    distribution?: string
    index?: number
    enabled?: boolean
  }
): Promise<void> {
  const response = await api.post(
    `/api/channel/${id}/workbuddy/settings`,
    settings
  )
  requireServerSuccess(response.data)
}
export async function workBuddyAction(
  id: number,
  index: number,
  action: 'refresh' | 'credits' | 'checkin'
): Promise<unknown> {
  const response = await api.post<{ success: boolean; data: unknown }>(
    `/api/channel/${id}/workbuddy/${index}/${action}`
  )
  return requireServerSuccess(response.data).data
}
