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
import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { DataTablePagination, DataTableView } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { TimingMetricsCell } from '@/features/usage-logs/components/timing-metrics-cell'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { getUsageRequests } from '../../usage-api'
import type {
  UsageQueryParams,
  UsageRequest,
  UsageScope,
} from '../../usage-types'
import { PanelWrapper } from '../ui/panel-wrapper'

export function UsageRequests(props: {
  params: UsageQueryParams
  scope: UsageScope
  userId?: number
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  useSystemConfigStore((state) => state.config.currency)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [selected, setSelected] = useState<UsageRequest | null>(null)
  const query = useQuery({
    queryKey: [
      'dashboard',
      'usage-requests',
      props.userId,
      props.scope,
      props.params,
      pagination,
    ],
    queryFn: () =>
      getUsageRequests(
        props.params,
        props.scope,
        pagination.pageIndex + 1,
        pagination.pageSize
      ),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const date = new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const number = (value: number | null) =>
    value === null ? t('Not reported') : formatNumber(value, locale)
  const quota = (value: number) =>
    formatQuotaWithCurrency(value, { locale, abbreviate: false })
  const timing = (row: UsageRequest) => (
    <TimingMetricsCell
      useTimeSec={row.use_time}
      completionTokens={row.output_tokens}
      frtMs={row.first_response_ms ?? undefined}
      isStream={row.is_stream}
      indicator='dot'
    />
  )
  const columns: ColumnDef<UsageRequest>[] = [
    {
      id: 'created_at',
      header: t('Time'),
      cell: ({ row }) => (
        <span className='text-xs whitespace-nowrap'>
          {date.format(new Date(row.original.created_at * 1000))}
        </span>
      ),
    },
    {
      id: 'token_name',
      header: t('Token name'),
      cell: ({ row }) => (
        <span
          className='inline-block max-w-36 truncate text-xs'
          title={row.original.token_name}
        >
          {row.original.token_name || t('Not reported')}
        </span>
      ),
    },
    {
      id: 'input',
      header: t('Input tokens'),
      cell: ({ row }) => number(row.original.input_tokens),
    },
    {
      id: 'output',
      header: t('Output tokens'),
      cell: ({ row }) => number(row.original.output_tokens),
    },
    {
      id: 'cache_read',
      header: t('Recorded cache read'),
      cell: ({ row }) => number(row.original.cache_read_tokens),
    },
    {
      id: 'cache_write',
      header: t('Recorded cache write'),
      cell: ({ row }) => number(row.original.cache_write_tokens),
    },
    {
      id: 'quota',
      header: t('API quota used'),
      cell: ({ row }) => (
        <span className='font-mono font-semibold'>
          {quota(row.original.quota)}
        </span>
      ),
    },
    {
      id: 'timing',
      header: t('Duration'),
      cell: ({ row }) => timing(row.original),
    },
    {
      id: 'mode',
      header: t('Mode'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-xs'>
          {row.original.is_stream ? t('Stream') : t('Non-stream')}
        </span>
      ),
    },
    {
      id: 'details',
      header: t('Details'),
      cell: ({ row }) => (
        <Button
          size='sm'
          variant='ghost'
          onClick={() => setSelected(row.original)}
        >
          {t('Details')}
        </Button>
      ),
    },
  ]
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => String(row.id),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  return (
    <section aria-label={t('Individual calls')} className='min-w-0'>
      <PanelWrapper
        title={t('Individual calls')}
        description={t(
          'Charged usage logs for this model, newest first. Uncharged errors are not included.'
        )}
      >
        {query.isError ? (
          <ErrorState
            title={t('Calls could not be loaded')}
            description={getServerErrorMessage(query.error)}
            onRetry={() => {
              void query.refetch()
            }}
          />
        ) : (
          <div
            className='flex min-w-0 flex-col gap-3'
            aria-busy={query.isFetching}
          >
            <DataTableView
              table={table}
              isLoading={query.isPending}
              tableClassName='min-w-[1100px]'
              emptyTitle={t('No calls on this page')}
            />
            {!query.isPending && <DataTablePagination table={table} compact />}
          </div>
        )}
      </PanelWrapper>
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('Call details')}
        description={selected?.model_name}
      >
        {selected && (
          <dl className='grid min-w-0 grid-cols-2 gap-x-4 gap-y-4 text-sm'>
            {[
              [t('Time'), date.format(new Date(selected.created_at * 1000))],
              [t('Token name'), selected.token_name || t('Not reported')],
              [t('Input tokens'), number(selected.input_tokens)],
              [t('Output tokens'), number(selected.output_tokens)],
              [t('Recorded cache read'), number(selected.cache_read_tokens)],
              [t('Recorded cache write'), number(selected.cache_write_tokens)],
              [
                t('Recorded cache share'),
                selected.cache_ratio === null
                  ? t('Not reported')
                  : `${formatNumber(selected.cache_ratio * 100, locale)}%`,
              ],
              [t('API quota used'), quota(selected.quota)],
              [t('Mode'), selected.is_stream ? t('Stream') : t('Non-stream')],
              [
                t('Usage data quality'),
                selected.ambiguous_requests
                  ? t('Ambiguous input accounting')
                  : t('Estimated usage'),
              ],
            ]
              .filter(
                ([label]) =>
                  label !== t('Usage data quality') ||
                  selected.estimated_requests > 0 ||
                  selected.ambiguous_requests > 0
              )
              .map(([label, value]) => (
                <div key={label} className='min-w-0'>
                  <dt className='text-muted-foreground mb-1 text-xs'>
                    {label}
                  </dt>
                  <dd className='font-mono text-xs break-all'>{value}</dd>
                </div>
              ))}
            <div className='col-span-2'>{timing(selected)}</div>
            <div className='col-span-2 min-w-0'>
              <dt className='text-muted-foreground mb-1 text-xs'>
                {t('Request ID')}
              </dt>
              <dd className='flex min-w-0 items-center gap-2'>
                <span className='min-w-0 font-mono text-xs break-all'>
                  {selected.request_id || t('Not reported')}
                </span>
                {selected.request_id && (
                  <CopyButton
                    value={selected.request_id}
                    aria-label={t('Copy request ID')}
                  />
                )}
              </dd>
            </div>
          </dl>
        )}
      </Dialog>
    </section>
  )
}
