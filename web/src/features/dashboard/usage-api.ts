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

import type {
  UsageDataset,
  UsageModelOption,
  UsageRequestPage,
  UsageQueryParams,
  UsageScope,
} from './usage-types'

export async function getModelUsage(
  params: UsageQueryParams,
  scope: UsageScope = 'self'
): Promise<UsageDataset> {
  const endpoint = scope === 'all' ? '/api/data/usage' : '/api/data/usage/self'
  const response = await api.get<{
    success: boolean
    message?: string
    data: UsageDataset
  }>(endpoint, { params })
  return requireServerSuccess(response.data).data
}

export async function getUsageModels(
  params: UsageQueryParams,
  scope: UsageScope
): Promise<UsageModelOption[]> {
  const endpoint = scope === 'all' ? '/api/data/usage' : '/api/data/usage/self'
  const response = await api.get<{
    success: boolean
    message?: string
    data: UsageModelOption[]
  }>(endpoint, {
    params: {
      ...params,
      model_name: undefined,
      start_timestamp: 1,
      view: 'models',
    },
  })
  return requireServerSuccess(response.data).data
}

export async function getUsageRequests(
  params: UsageQueryParams,
  scope: UsageScope,
  page: number,
  pageSize: number
): Promise<UsageRequestPage> {
  const endpoint = scope === 'all' ? '/api/data/usage' : '/api/data/usage/self'
  const response = await api.get<{
    success: boolean
    message?: string
    data: UsageRequestPage
  }>(endpoint, {
    params: { ...params, view: 'requests', page, page_size: pageSize },
  })
  return requireServerSuccess(response.data).data
}
