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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { channelSchema } from '../../types'
import { BalanceCell } from '../channels-columns'
import { ChannelsDialogs } from '../channels-dialogs'
import { ChannelsProvider } from '../channels-provider'
import { WorkBuddyDialog } from '../dialogs/workbuddy-dialog'

const originalAuth = useAuthStore.getState().auth
let client: QueryClient
function state() {
  return {
    distribution: 'priority',
    autoCheckin: true,
    accounts: [
      {
        index: 0,
        nickname: 'Personal account',
        enabled: true,
        region: 'cn',
        expiresAtMs: 0,
        checkinDay: '2026-10-08',
      },
    ],
  }
}
function panel() {
  return (
    <QueryClientProvider client={client}>
      <WorkBuddyDialog
        channelId={64}
        channelName='Test WorkBuddy'
        onClose={vi.fn()}
      />
    </QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: { id: 1, username: 'root', role: ROLE.SUPER_ADMIN },
    },
  })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  useAuthStore.setState({ auth: originalAuth })
})
it('saves automatic check-in settings and displays the refreshed state', async () => {
  const data = state()
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data },
  }))
  const post = vi.spyOn(api, 'post').mockImplementation(async (_url, value) => {
    data.autoCheckin = (value as { autoCheckin: boolean }).autoCheckin
    return { data: { success: true } }
  })
  render(panel())
  const auto = await screen.findByRole('switch', {
    name: 'Automatic check-in: 09:00 (Japan time)',
  })
  expect(auto).toBeChecked()
  await userEvent.setup().click(auto)
  await waitFor(() => expect(auto).not.toBeChecked())
  expect(post).toHaveBeenCalledWith('/api/channel/64/workbuddy/settings', {
    autoCheckin: false,
  })
  expect(screen.getByText('Last check-in day: 2026-10-08')).toBeInTheDocument()
})
it('keeps account settings unchanged when a settings request fails', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: state() },
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: false, message: 'Settings changed concurrently' },
  })
  render(panel())
  const account = await screen.findByRole('switch', {
    name: 'Personal account',
  })
  await userEvent.setup().click(account)
  await waitFor(() => expect(account).toBeEnabled())
  expect(account).toBeChecked()
})
it('disables check-in for a disabled account but permits credential recovery', async () => {
  const data = state()
  data.accounts[0].enabled = false
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data } })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: { ok: true } } })
  render(panel())
  expect(
    await screen.findByRole('button', { name: 'Check in now' })
  ).toBeDisabled()
  const refresh = screen.getByRole('button', { name: 'Refresh credential' })
  expect(refresh).toBeEnabled()
  await userEvent.setup().click(refresh)
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/channel/64/workbuddy/0/refresh')
  )
})
it('disables every account mutation for a read-only channel operator', async () => {
  useAuthStore.setState({
    auth: {
      ...originalAuth,
      user: {
        id: 2,
        username: 'reader',
        role: ROLE.ADMIN,
        permissions: { admin_permissions: { channel: { read: true } } },
      },
    },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: state() },
  })
  render(panel())
  await screen.findByText('Personal account')
  for (const control of screen.getAllByRole('switch')) {
    expect(control).toHaveAttribute('aria-disabled', 'true')
  }
  for (const name of ['Query Balance', 'Check in now', 'Refresh credential']) {
    expect(screen.getByRole('button', { name })).toBeDisabled()
  }
})
it('shows a failed account load and permits an explicit retry', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('Connection unavailable'))
  render(panel())
  const retry = await screen.findByRole('button', { name: 'Retry' })
  get.mockResolvedValueOnce({ data: { success: true, data: state() } })
  await userEvent.setup().click(retry)
  expect(await screen.findByText('Personal account')).toBeInTheDocument()
})
it('shows an empty state when there are no accounts', async () => {
  const data = state()
  data.accounts = []
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data } })
  render(panel())
  expect(await screen.findByText('No Data')).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Check in now' })
  ).not.toBeInTheDocument()
})

it('opens WorkBuddy accounts from the balance cell instead of calling the generic balance API', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/channel/64/workbuddy' ? state() : [],
    },
  }))
  const channel = channelSchema.parse({
    id: 64,
    type: 64,
    name: 'Native WorkBuddy',
    key: '',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <BalanceCell channel={channel} />
        <ChannelsDialogs />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const entry = screen.getByRole('button', { name: 'WorkBuddy accounts' })
  expect(entry).toHaveAttribute('aria-haspopup', 'dialog')
  await userEvent.setup().click(entry)
  expect(
    await screen.findByRole('dialog', { name: 'WorkBuddy accounts' })
  ).toBeInTheDocument()
  expect(await screen.findByText('Personal account')).toBeInTheDocument()
  expect(get.mock.calls.some(([url]) => url.includes('update_balance'))).toBe(
    false
  )
})
