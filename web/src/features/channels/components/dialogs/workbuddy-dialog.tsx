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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  getWorkBuddyState,
  updateWorkBuddySettings,
  workBuddyAction,
} from '../../workbuddy-api'

export function WorkBuddyDialog(props: {
  channelId: number
  channelName: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [result, setResult] = useState<unknown>()
  const user = useAuthStore((state) => state.auth.user)
  const canManage = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const key = ['channels', props.channelId, 'workbuddy']
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getWorkBuddyState(props.channelId, signal),
    retry: false,
    gcTime: 0,
  })
  const settings = useMutation({
    mutationFn: (value: Parameters<typeof updateWorkBuddySettings>[1]) =>
      updateWorkBuddySettings(props.channelId, value),
    onSuccess: () => client.invalidateQueries({ queryKey: key }),
  })
  const action = useMutation({
    mutationFn: (value: {
      index: number
      action: 'refresh' | 'credits' | 'checkin'
    }) => workBuddyAction(props.channelId, value.index, value.action),
    onSuccess: (data) => {
      setResult(data)
      void client.invalidateQueries({ queryKey: key })
    },
  })
  const busy = settings.isPending || action.isPending
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('WorkBuddy accounts')}
      description={props.channelName}
    >
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          onRetry={() => {
            void query.refetch()
          }}
        />
      )}
      {query.data && (
        <div className='space-y-4'>
          <div className='flex items-center justify-between gap-3'>
            <Label htmlFor='workbuddy-auto-checkin'>
              {t('Automatic check-in: 09:00 (Japan time)')}
            </Label>
            <Switch
              id='workbuddy-auto-checkin'
              checked={query.data.autoCheckin}
              disabled={!canManage || busy}
              onCheckedChange={(value) =>
                settings.mutate({ autoCheckin: value })
              }
            />
          </div>
          <div className='flex items-center justify-between gap-3'>
            <Label htmlFor='workbuddy-round-robin'>
              {t('Rotate accounts')}
            </Label>
            <Switch
              id='workbuddy-round-robin'
              checked={query.data.distribution === 'round-robin'}
              disabled={!canManage || busy}
              onCheckedChange={(value) =>
                settings.mutate({
                  distribution: value ? 'round-robin' : 'priority',
                })
              }
            />
          </div>
          {!query.data.accounts.length && <EmptyState />}
          {query.data.accounts.map((account) => (
            <section
              key={account.index}
              className='space-y-3 rounded-md border p-3'
            >
              <div className='flex items-center justify-between gap-3'>
                <Label htmlFor={`workbuddy-account-${account.index}`}>
                  {account.nickname || t('WorkBuddy account')}
                </Label>
                <Switch
                  id={`workbuddy-account-${account.index}`}
                  checked={account.enabled}
                  disabled={!canManage || busy}
                  onCheckedChange={(value) =>
                    settings.mutate({ index: account.index, enabled: value })
                  }
                />
              </div>
              <p className='text-muted-foreground text-sm'>
                {t('Last check-in day')}:{' '}
                {account.checkinDay || t('Not checked')}
              </p>
              <div className='flex flex-wrap gap-2'>
                <Button
                  variant='outline'
                  disabled={!canManage || busy}
                  onClick={() =>
                    action.mutate({ index: account.index, action: 'credits' })
                  }
                >
                  {t('Query Balance')}
                </Button>
                <Button
                  variant='outline'
                  disabled={!canManage || busy || !account.enabled}
                  onClick={() =>
                    action.mutate({ index: account.index, action: 'checkin' })
                  }
                >
                  {t('Check in now')}
                </Button>
                <Button
                  variant='outline'
                  disabled={!canManage || busy}
                  onClick={() =>
                    action.mutate({ index: account.index, action: 'refresh' })
                  }
                >
                  {t('Refresh credential')}
                </Button>
              </div>
            </section>
          ))}
          {result !== undefined && (
            <CodeBlock code={JSON.stringify(result, null, 2)} language='json'>
              <CodeBlockCopyButton />
            </CodeBlock>
          )}
        </div>
      )}
    </Dialog>
  )
}
