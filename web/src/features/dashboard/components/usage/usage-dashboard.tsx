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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, RefreshCw } from 'lucide-react'
import { lazy, Suspense, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatCompactNumber,
  formatNumber,
  formatTimestampToDate,
} from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  buildUsageFilters,
  buildUsageQueryParams,
  prepareUsageData,
} from '../../lib/usage'
import { getModelUsage, getUsageModels } from '../../usage-api'
import type {
  UsageFilters as UsageFilterValues,
  UsageScope,
} from '../../usage-types'
import { ModelUsageDetail } from './model-usage-detail'
import { UsageFilters } from './usage-filters'
import { UsageModels } from './usage-models'
import { UsageRequests } from './usage-requests'
import { UsageSummary } from './usage-summary'

const LazyUsageCharts = lazy(() =>
  import('./usage-charts').then((module) => ({ default: module.UsageCharts }))
)

interface UsageDashboardProps {
  compact?: boolean
  singleModel?: boolean
}

export function UsageDashboard(props: UsageDashboardProps) {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const isAdmin = Boolean(user && user.role >= ROLE.ADMIN)
  const compact = Boolean(props.compact)
  const [scope, setScope] = useState<UsageScope>('self')
  const [rangeDays, setRangeDays] = useState<number | null>(1)
  const [filters, setFilters] = useState<UsageFilterValues>(() =>
    buildUsageFilters()
  )
  const effectiveScope = !compact && isAdmin ? scope : 'self'
  const catalogParams = useMemo(
    () =>
      buildUsageQueryParams(
        { ...filters, model: '', start: new Date(1000) },
        effectiveScope
      ),
    [filters, effectiveScope]
  )
  const catalogQuery = useQuery({
    queryKey: [
      'dashboard',
      'usage-models',
      user?.id,
      effectiveScope,
      catalogParams,
    ],
    queryFn: () => getUsageModels(catalogParams, effectiveScope),
    enabled: Boolean(props.singleModel),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const selectedModel =
    filters.model || catalogQuery.data?.[0]?.model_name || ''
  const params = useMemo(() => {
    const query = buildUsageQueryParams(filters, effectiveScope)
    // Catalog values are exact stored names, including any surrounding whitespace.
    if (props.singleModel) query.model_name = selectedModel
    return query
  }, [filters, effectiveScope, props.singleModel, selectedModel])
  const usageQuery = useQuery({
    queryKey: ['dashboard', 'model-usage', user?.id, effectiveScope, params],
    queryFn: () => getModelUsage(params, effectiveScope),
    enabled: !props.singleModel || Boolean(selectedModel),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const data = useMemo(
    () => (usageQuery.data ? prepareUsageData(usageQuery.data) : undefined),
    [usageQuery.data]
  )

  const handleRangeChange = (value: string) => {
    const days = Number(value)
    if (![0, 1, 7, 30, 90, 365].includes(days)) return
    setRangeDays(days)
    setFilters((current) => ({
      ...buildUsageFilters(days),
      model: current.model,
      username: current.username,
    }))
  }

  let notice: ReactNode = null
  if (props.singleModel && catalogQuery.isError) {
    notice = (
      <ErrorState
        title={t('Model list could not be loaded')}
        description={getServerErrorMessage(catalogQuery.error)}
        onRetry={() => {
          void catalogQuery.refetch()
        }}
      />
    )
  } else if (props.singleModel && !selectedModel && !catalogQuery.isPending) {
    notice = (
      <EmptyState
        title={t('No recorded models')}
        description={t(
          'Models appear here after calls are recorded in usage logs.'
        )}
        bordered
      />
    )
  } else if (usageQuery.isError) {
    notice = (
      <ErrorState
        title={t('Model usage could not be loaded')}
        description={getServerErrorMessage(
          usageQuery.error,
          t('Retry to load statistics for this time range.')
        )}
        onRetry={() => {
          void usageQuery.refetch()
        }}
      />
    )
  }

  return (
    <section
      className='flex min-w-0 flex-col gap-4'
      aria-label={
        compact ? t('Model usage at a glance') : t('Model usage analytics')
      }
    >
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <h2 className='text-lg font-semibold'>
            {compact
              ? t('Model usage at a glance')
              : t(
                  props.singleModel
                    ? 'Analyze one model at a time'
                    : 'Usage breakdown'
                )}
          </h2>
          <p className='text-muted-foreground text-xs'>
            {compact
              ? t('Your model tokens and cache usage in the last 24 hours')
              : t(
                  props.singleModel
                    ? 'Choose a model to inspect its quota, tokens, cache and individual calls.'
                    : 'Understand what you send, what models generate, and what the cache records.'
                )}
          </p>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          {!compact && isAdmin && (
            <Select
              value={effectiveScope}
              onValueChange={(value) => {
                if (value === 'self' || value === 'all') {
                  setScope(value)
                  if (props.singleModel) {
                    setFilters((current) => ({ ...current, model: '' }))
                  }
                }
              }}
            >
              <SelectTrigger size='sm' aria-label={t('Usage scope')}>
                <SelectValue>
                  {effectiveScope === 'self'
                    ? t('My usage')
                    : t('All accounts')}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='self'>{t('My usage')}</SelectItem>
                  <SelectItem value='all'>{t('All accounts')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          )}
          {compact && (
            <Button
              variant='outline'
              size='sm'
              render={
                <Link to='/dashboard/$section' params={{ section: 'models' }} />
              }
            >
              {t('View usage analysis')}
              <ArrowRight data-icon='inline-end' />
            </Button>
          )}
          <Button
            variant='ghost'
            size='icon'
            className='size-8'
            aria-label={t('Refresh usage')}
            disabled={
              usageQuery.isFetching || (props.singleModel && !selectedModel)
            }
            onClick={() => {
              void queryClient.invalidateQueries({
                queryKey: [
                  'dashboard',
                  'usage-requests',
                  user?.id,
                  effectiveScope,
                ],
              })
              if (props.singleModel) {
                void catalogQuery.refetch()
              }
              if (rangeDays === null) {
                void usageQuery.refetch()
                return
              }
              const fresh = {
                ...filters,
                ...buildUsageFilters(rangeDays),
                model: filters.model,
                username: filters.username,
              }
              const freshParams = buildUsageQueryParams(fresh, effectiveScope)
              if (
                freshParams.start_timestamp === params.start_timestamp &&
                freshParams.end_timestamp === params.end_timestamp
              ) {
                void usageQuery.refetch()
              } else {
                setFilters(fresh)
              }
            }}
          >
            <RefreshCw
              className={cn(usageQuery.isFetching && 'animate-spin')}
            />
          </Button>
        </div>
      </div>
      {props.singleModel && (
        <div className='bg-card flex min-w-0 flex-col gap-2 rounded-2xl border p-4 sm:flex-row sm:items-center sm:gap-4 sm:p-5'>
          <Label htmlFor='usage-model-selector' className='shrink-0'>
            {t('Model to analyze')}
          </Label>
          <Combobox
            id='usage-model-selector'
            aria-label={t('Model to analyze')}
            value={selectedModel}
            options={(catalogQuery.data ?? []).map((model) => ({
              value: model.model_name,
              label: model.model_name,
            }))}
            onValueChange={(model) => {
              if (model) setFilters((current) => ({ ...current, model }))
            }}
            className='w-full min-w-0 sm:max-w-md'
            placeholder={t('Select a model')}
            searchPlaceholder={t('Search models')}
            emptyText={t('No matching models')}
            disabled={catalogQuery.isPending || catalogQuery.isError}
          />
          <span className='text-muted-foreground text-xs'>
            {t('Only the selected model is included below.')}
          </span>
        </div>
      )}
      {!compact && (
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <Tabs
            value={rangeDays === null ? '' : String(rangeDays)}
            onValueChange={handleRangeChange}
            className='max-w-full min-w-0'
          >
            <TabsList
              aria-label={t('Usage time range')}
              className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'
            >
              <TabsTrigger value='1'>{t('Last 24 hours')}</TabsTrigger>
              <TabsTrigger value='7'>{t('Last 7 days')}</TabsTrigger>
              <TabsTrigger value='30'>{t('Last 30 days')}</TabsTrigger>
              <TabsTrigger value='90'>{t('Last 90 days')}</TabsTrigger>
              <TabsTrigger value='365'>{t('Last year')}</TabsTrigger>
              <TabsTrigger value='0'>{t('All history')}</TabsTrigger>
            </TabsList>
          </Tabs>
          <UsageFilters
            filters={{
              ...filters,
              model: props.singleModel ? selectedModel : filters.model,
            }}
            singleModel={props.singleModel}
            allAccounts={effectiveScope === 'all'}
            onReset={() => {
              setFilters({
                ...buildUsageFilters(),
                model: props.singleModel ? selectedModel : '',
              })
              setRangeDays(1)
            }}
            onApply={(next) => {
              setFilters(next)
              setRangeDays(null)
            }}
          />
        </div>
      )}
      {!compact && (
        <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
          <span>
            {formatTimestampToDate(
              usageQuery.data?.meta.start_timestamp ?? params.start_timestamp
            )}{' '}
            – {formatTimestampToDate(params.end_timestamp)}
          </span>
          <Badge variant='outline'>
            {effectiveScope === 'self' ? t('My usage') : t('All accounts')}
          </Badge>
          <span>
            {t(
              'Statistics include retained logs only. Long ranges use daily or monthly buckets.'
            )}
          </span>
          {params.model_name && (
            <Badge
              variant='outline'
              className='max-w-full min-w-0'
              title={params.model_name}
            >
              <span className='min-w-0 truncate'>{params.model_name}</span>
            </Badge>
          )}
          {params.username && (
            <Badge
              variant='outline'
              className='max-w-full min-w-0'
              title={params.username}
            >
              <span className='min-w-0 truncate'>{params.username}</span>
            </Badge>
          )}
        </div>
      )}
      {notice ?? (
        <>
          <UsageSummary
            summary={data?.summary}
            series={data?.series}
            loading={usageQuery.isPending}
            compact={compact}
          />
          {data && (
            <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs'>
              <span>
                {t('Total tokens')}:{' '}
                <strong className='text-foreground font-mono'>
                  {formatCompactNumber(data.summary.total_tokens, locale)}
                </strong>
              </span>
              {!props.singleModel && (
                <span>
                  {t('Models')}: {formatNumber(data.models.length, locale)}
                </span>
              )}
              <span>
                {t('Requests')}:{' '}
                {formatNumber(data.summary.request_count, locale)}
              </span>
              <span>
                {t(
                  'Cache data recorded for {{recorded}} of {{total}} requests.',
                  {
                    recorded: formatNumber(
                      data.summary.cache_recorded_requests,
                      locale
                    ),
                    total: formatNumber(data.summary.request_count, locale),
                  }
                )}
              </span>
            </div>
          )}
          {data &&
            (data.summary.estimated_requests > 0 ||
              data.summary.ambiguous_requests > 0 ||
              usageQuery.data?.meta.incomplete) && (
              <Alert>
                <AlertTitle>{t('Usage data quality')}</AlertTitle>
                <AlertDescription>
                  {data.summary.estimated_requests > 0 && (
                    <p>
                      {t('{{count}} requests contain estimated token usage.', {
                        count: data.summary.estimated_requests,
                      })}
                    </p>
                  )}
                  {data.summary.ambiguous_requests > 0 && (
                    <p>
                      {t(
                        '{{count}} requests have ambiguous input accounting and are excluded from input and cache totals.',
                        { count: data.summary.ambiguous_requests }
                      )}
                    </p>
                  )}
                  {usageQuery.data?.meta.incomplete && (
                    <p>
                      {t(
                        'This range contains incomplete log data. Totals may be lower than actual usage.'
                      )}
                    </p>
                  )}
                </AlertDescription>
              </Alert>
            )}
          {props.singleModel && data && data.summary.request_count > 0 && (
            <>
              <ModelUsageDetail
                model={selectedModel}
                summary={data.summary}
                series={data.series}
                granularity={
                  usageQuery.data?.meta.granularity ?? filters.granularity
                }
              />
              <UsageRequests
                key={JSON.stringify([user?.id, effectiveScope, params])}
                params={params}
                scope={effectiveScope}
                userId={user?.id}
              />
            </>
          )}
          {usageQuery.isPending && (
            <div
              className='grid gap-4 lg:grid-cols-2'
              aria-label={t('Loading usage analysis')}
            >
              <Skeleton className='h-80 w-full rounded-xl' />
              <Skeleton className='h-80 w-full rounded-xl' />
            </div>
          )}
          {!usageQuery.isPending && data?.summary.request_count === 0 && (
            <EmptyState
              title={t('No model usage in this time range')}
              description={t(
                'Usage analysis appears after requests are recorded in usage logs.'
              )}
              bordered
            />
          )}
          {!usageQuery.isPending &&
            data &&
            data.summary.request_count > 0 &&
            !props.singleModel && (
              <>
                <div className='grid min-w-0 items-start gap-4 lg:grid-cols-2'>
                  <Suspense
                    fallback={<Skeleton className='h-80 w-full rounded-xl' />}
                  >
                    <LazyUsageCharts
                      series={data.series}
                      granularity={
                        usageQuery.data?.meta.granularity ?? filters.granularity
                      }
                    />
                  </Suspense>
                  <UsageModels
                    models={data.models}
                    totalTokens={data.summary.total_tokens}
                    totalQuota={data.summary.quota}
                    compact
                  />
                </div>
                {!compact && (
                  <>
                    <Suspense
                      fallback={<Skeleton className='h-80 w-full rounded-xl' />}
                    >
                      <LazyUsageCharts
                        series={data.series}
                        granularity={
                          usageQuery.data?.meta.granularity ??
                          filters.granularity
                        }
                        cacheOnly
                      />
                    </Suspense>
                    <UsageModels
                      models={data.models}
                      totalTokens={data.summary.total_tokens}
                      totalQuota={data.summary.quota}
                    />
                  </>
                )}
              </>
            )}
        </>
      )}
    </section>
  )
}
