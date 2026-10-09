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
import { useTranslation } from 'react-i18next'

import { CodeBlock } from '@/components/ai-elements/code-block'
import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { formatNumber } from '@/lib/format'

import type {
  DiagnosticAttempt,
  DiagnosticExchange,
  ErrorDiagnostics,
} from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

function attemptKey(attempt: DiagnosticAttempt): string {
  return String(
    attempt.id ??
      [
        attempt.channel_id,
        attempt.request.url,
        attempt.upstream_request_id,
        attempt.elapsed_ms,
      ].join(':')
  )
}

function DiagnosticExchangeView(props: {
  exchange: DiagnosticExchange
  label: string
}) {
  const { t } = useTranslation()
  const exchange = props.exchange
  let body = exchange.body.data
  let language = 'plaintext'
  if (exchange.body.encoding === 'utf-8') {
    try {
      body = JSON.stringify(JSON.parse(body), null, 2)
      language = 'json'
    } catch {
      /* HTML, SSE and plain-text errors remain unchanged. */
    }
  }
  return (
    <DetailSection label={props.label}>
      {exchange.url && (
        <DetailRow
          label={t('Endpoint')}
          value={`${exchange.method ?? ''} ${exchange.url}`}
          mono
        />
      )}
      {exchange.status != null && (
        <DetailRow label={t('HTTP Status')} value={exchange.status} mono />
      )}
      {exchange.protocol && (
        <DetailRow label={t('Protocol')} value={exchange.protocol} mono />
      )}
      {(exchange.body.truncated || exchange.headers_truncated) && (
        <Alert>
          <AlertTitle>{t('Capture limit reached')}</AlertTitle>
          <AlertDescription>
            {t(
              'Captured {{captured}} of {{observed}} observed body bytes. Headers and bodies may be incomplete.',
              {
                captured: formatNumber(exchange.body.captured_bytes),
                observed: formatNumber(exchange.body.bytes),
              }
            )}
          </AlertDescription>
        </Alert>
      )}
      {exchange.body.read_error && (
        <DetailRow
          label={t('Body read error')}
          value={exchange.body.read_error}
          mono
        />
      )}
      {Object.keys(exchange.headers ?? {}).length > 0 && (
        <CodeBlock
          language='json'
          code={JSON.stringify(exchange.headers, null, 2)}
          title={t('Headers')}
          showToolbar
          maxExpandedLines={8}
        >
          <CopyButton
            value={JSON.stringify(exchange.headers, null, 2)}
            className='size-8'
            aria-label={t('Copy headers')}
          />
        </CodeBlock>
      )}
      {exchange.body.data ? (
        <CodeBlock
          language={language}
          code={body}
          title={
            exchange.body.encoding === 'base64' ? t('Body (Base64)') : t('Body')
          }
          showToolbar
          maxExpandedLines={14}
        >
          <CopyButton
            value={exchange.body.data}
            className='size-8'
            aria-label={t('Copy original body')}
          />
        </CodeBlock>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {t('No body bytes were captured.')}
        </p>
      )}
    </DetailSection>
  )
}

export function ErrorDiagnosticsPanel(props: {
  diagnostics: ErrorDiagnostics
}) {
  const { t } = useTranslation()
  const data = props.diagnostics
  const report = JSON.stringify(data, null, 2)
  const lastAttempt = Math.max(0, data.attempts.length - 1)
  const stages: Record<string, string> = {
    routing: t('Channel routing'),
    validation: t('Request validation'),
    relay_info: t('Relay preparation'),
    billing: t('Billing validation'),
    request_conversion: t('Request conversion'),
    upstream_request: t('Upstream connection'),
    upstream_response: t('Upstream response'),
  }
  return (
    <DetailSection label={t('Error diagnostics')} variant='danger'>
      <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-xs'>
          {t('Original data captured for this failed request.')}
        </p>
        <CopyButton
          value={report}
          variant='outline'
          size='sm'
          aria-label={t('Copy diagnostic report')}
        >
          {t('Copy diagnostic report')}
        </CopyButton>
      </div>
      <DetailRow
        label={t('Failure stage')}
        value={stages[data.stage] ?? data.stage}
      />
      <DetailRow
        label={t('Client status')}
        value={`HTTP ${data.status}`}
        mono
      />
      {data.error_code && (
        <DetailRow label={t('Error code')} value={data.error_code} mono />
      )}
      <DetailRow
        label={t('Elapsed time')}
        value={`${formatNumber(data.elapsed_ms)} ms`}
        mono
      />
      <CodeBlock
        language='plaintext'
        code={data.error}
        title={t('Original error')}
        maxExpandedLines={3}
        showToolbar
      >
        <CopyButton
          value={data.error}
          className='size-8'
          aria-label={t('Copy original error')}
        />
      </CodeBlock>
      <Tabs
        defaultValue={data.attempts.length ? 'upstream' : 'request'}
        className='mt-4 min-w-0'
      >
        <div className='max-w-full overflow-x-auto'>
          <TabsList aria-label={t('Diagnostic views')}>
            <TabsTrigger value='request'>{t('Original request')}</TabsTrigger>
            <TabsTrigger value='upstream'>{t('Upstream attempts')}</TabsTrigger>
            <TabsTrigger value='report'>{t('Diagnostic JSON')}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value='request'>
          <DiagnosticExchangeView
            exchange={data.client_request}
            label={t('Original request')}
          />
        </TabsContent>
        <TabsContent value='upstream'>
          {data.omitted_attempts ? (
            <p className='text-xs text-amber-600'>
              {t(
                '{{count}} additional upstream attempts exceeded the capture limit.',
                { count: data.omitted_attempts }
              )}
            </p>
          ) : null}
          {data.attempts.length ? (
            <Tabs defaultValue={String(lastAttempt)} className='min-w-0'>
              <div className='max-w-full overflow-x-auto'>
                <TabsList aria-label={t('Upstream attempts')}>
                  {data.attempts.map((attempt, index) => (
                    <TabsTrigger
                      key={attemptKey(attempt)}
                      value={String(index)}
                    >
                      {t('Attempt')} {index + 1}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </div>
              {data.attempts.map((attempt, index) => (
                <TabsContent
                  key={attemptKey(attempt)}
                  value={String(index)}
                  className='space-y-3'
                >
                  <DetailRow
                    label={t('Channel')}
                    value={`#${attempt.channel_id} ${attempt.channel_name ?? ''}`}
                  />
                  {attempt.model && (
                    <DetailRow
                      label={t('Actual Model')}
                      value={attempt.model}
                      mono
                    />
                  )}
                  {attempt.request_format && (
                    <DetailRow
                      label={t('Request Format')}
                      value={attempt.request_format}
                      mono
                    />
                  )}
                  {attempt.upstream_request_id && (
                    <DetailRow
                      label={t('Upstream Request ID')}
                      value={attempt.upstream_request_id}
                      mono
                    />
                  )}
                  <DetailRow
                    label={t('Elapsed time')}
                    value={`${formatNumber(attempt.elapsed_ms)} ms`}
                    mono
                  />
                  {attempt.error && (
                    <CodeBlock
                      code={attempt.error}
                      language='plaintext'
                      title={t('Attempt error')}
                      maxExpandedLines={3}
                    />
                  )}
                  <div className='grid min-w-0 gap-4 lg:grid-cols-2'>
                    <DiagnosticExchangeView
                      exchange={attempt.request}
                      label={t('Forwarded request')}
                    />
                    <DiagnosticExchangeView
                      exchange={attempt.response}
                      label={t('Upstream response')}
                    />
                  </div>
                </TabsContent>
              ))}
            </Tabs>
          ) : (
            <p className='text-muted-foreground text-xs'>
              {t('The request failed before an upstream connection was made.')}
            </p>
          )}
        </TabsContent>
        <TabsContent value='report'>
          <CodeBlock
            code={report}
            language='json'
            title={t('Diagnostic JSON')}
            showToolbar
            maxExpandedLines={20}
          >
            <CopyButton
              value={report}
              className='size-8'
              aria-label={t('Copy diagnostic JSON')}
            />
          </CodeBlock>
        </TabsContent>
      </Tabs>
    </DetailSection>
  )
}
