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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterAll, afterEach, beforeEach, expect, test, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { UsageLog } from '../../data/schema'
import type { ErrorDiagnostics, LogOtherData } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { DetailsDialog } from '../dialogs/details-dialog'

vi.mock('@lobehub/icons', () => ({}))
vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})
afterAll(() => vi.unstubAllGlobals())

function makeLog(other: LogOtherData, type = 2): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'wan2.5-i2v-preview',
    quota: 5000,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function DetailPreview(props: {
  other: LogOtherData
  isAdmin: boolean
  type?: number
}) {
  const table = useReactTable({
    data: [makeLog(props.other, props.type)],
    columns: useCommonLogsColumns(props.isAdmin, false),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'content')
  if (!cell) throw new Error('The log must have a content column')
  return flexRender(cell.column.columnDef.cell, cell.getContext())
}
const plugin = {
  key: 'incho',
  name: 'Incho',
  version: '1.0.1',
  author: { name: 'Plugin maintainer' },
}
const previousConfig = useSystemConfigStore.getState().config
let client: QueryClient
const i18n = createInstance()
beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: { en },
    interpolation: { escapeValue: false },
  })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  client.setQueryData(
    ['pricing'],
    { data: [], vendors: [] },
    { updatedAt: Date.now() + 60_000 }
  )
})
afterEach(() => {
  client.clear()
  useSystemConfigStore.getState().setConfig(previousConfig)
})
function renderPreview(other: LogOtherData, isAdmin = true, type?: number) {
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <DetailPreview other={other} isAdmin={isAdmin} type={type} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return screen.getByRole('button', { name: /./ })
}

test('keeps log details open when the parent refreshes with unchanged data', async () => {
  const other = { model_price: 0.25 }
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </I18nextProvider>
  )
  const { rerender } = render(<DetailPreview other={other} isAdmin />, {
    wrapper,
  })
  fireEvent.click(screen.getByRole('button', { name: 'Per-call · $0.25' }))
  expect(await screen.findByRole('dialog')).toBeVisible()
  rerender(<DetailPreview other={other} isAdmin />)
  expect(screen.getByRole('dialog')).toBeVisible()
})

test.each([
  {
    name: 'fixed expression zero price',
    other: {
      billing_mode: 'tiered_expr',
      billing_unit: 'request' as const,
      fixed_price: 0,
      matched_tier: 'free',
      expr_b64: btoa('tier("free", fixed(0))'),
    },
    expected: 'free · Per-call $0/request',
  },
  {
    name: 'fixed expression trace outside the display grammar',
    other: {
      billing_mode: 'tiered_expr',
      billing_unit: 'request' as const,
      fixed_price: 0.01,
      matched_tier: 'priority',
      expr_b64: btoa(
        'param("fast") == true ? tier("priority", fixed(0.01)) : tier("tokens", p * 2)'
      ),
    },
    expected: 'priority · Per-call $0.01/request',
  },
  {
    name: 'per-call',
    other: { model_price: 0.25 },
    expected: 'Per-call · $0.25',
  },
  {
    name: 'standard',
    other: { model_ratio: 1, completion_ratio: 2 },
    expected: 'Standard · $2 / $4/M',
  },
  {
    name: 'zero price fallback',
    other: { model_price: 0, group_ratio: 1 },
    expected: 'Group Ratio 1x',
  },
  { name: 'missing price fallback', other: {}, expected: '—' },
])('$name stays visible without a plugin counter', ({ other, expected }) => {
  const preview = renderPreview({
    ...other,
    admin_info: { task_plugin: plugin },
  })
  expect(preview.textContent).toBe(expected)
})

test('top-up content_parts render in the viewer language', async () => {
  i18n.addResourceBundle('zh', 'translation', {
    'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}':
      '在线充值成功，充值额度：{{quota}}，支付金额：{{amount}}',
  })
  await i18n.changeLanguage('zh')
  const preview = renderPreview(
    {
      content_parts: [
        {
          key: 'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
          params: { quota: '$10.000000', amount: '10.00' },
        },
      ],
    },
    false,
    1
  )
  expect(preview.textContent).toBe(
    '在线充值成功，充值额度：$10.000000，支付金额：10.00'
  )
})

test('quota saturation remains first and only billing adds to the counter', () => {
  const preview = renderPreview({
    model_price: 0.25,
    admin_info: {
      task_plugin: plugin,
      quota_saturation: {
        op: 'round',
        kind: 'overflow',
        original: 3e9,
        clamped: 2147483647,
      },
    },
  })
  expect(preview.textContent).toBe('Quota clamped+1')
})

test.each([true, false])(
  'plugin information in the opened dialog respects admin=%s',
  async (isAdmin) => {
    const preview = renderPreview(
      { model_price: 0.25, admin_info: { task_plugin: plugin } },
      isAdmin
    )
    expect(preview.textContent).toBe('Per-call · $0.25')
    fireEvent.click(preview)
    const dialog = within(await screen.findByRole('dialog'))
    if (isAdmin) {
      expect(dialog.getByText('Incho')).toBeVisible()
      expect(dialog.getByText('1.0.1')).toBeVisible()
      expect(dialog.getByText('Plugin maintainer')).toBeVisible()
    } else {
      expect(dialog.queryByText('Incho')).not.toBeInTheDocument()
      expect(dialog.queryByText('Plugin maintainer')).not.toBeInTheDocument()
    }
  }
)

test.each([
  {
    expression: 'tier("music", u("clips") * 0.25)',
    tier: 'music',
    expected: 'music · clips $0.25/unit',
  },
  {
    expression:
      'u("mode") == "pro" ? tier("pro", u("seconds") * 0.8) : tier("std", u("seconds") * 0.4)',
    tier: 'pro',
    expected: 'pro · seconds $0.8/second',
  },
  {
    expression: 'tier("tokens", u("tokens") * 9.8 / 1000000)',
    tier: 'tokens',
    expected: 'tokens · tokens $9.8/1M token',
  },
  {
    expression: 'tier("free", u("clips") * 0)',
    tier: 'free',
    expected: 'free · clips $0/unit',
  },
  {
    expression: 'tier("mixed", 0.1 + u("clips") * 0.25 + u("units") * 0.14)',
    tier: 'mixed',
    expected:
      'mixed · clips $0.25/unit · units $0.14/credit · Additional charge $0.1/request',
  },
])(
  'task expression $tier shows its recorded unit price',
  ({ expression, tier, expected }) => {
    client.setQueryData(['pricing'], {
      data: [
        {
          model_name: 'wan2.5-i2v-preview',
          billing_expr: 'tier("current", u("clips") * 99)',
          billing_usage_schema: {
            clips: { type: 'number', unit: 'count' },
            seconds: { type: 'number', unit: 'second' },
            tokens: { type: 'number', unit: 'token' },
            units: { type: 'number', unit: 'credit' },
            mode: { enum: ['pro', 'std'] },
          },
        },
      ],
      vendors: [],
    })
    const preview = renderPreview({
      is_task: true,
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expression).toString('base64'),
      matched_tier: tier,
      model_price: 0,
      admin_info: { task_plugin: plugin },
    })
    expect(preview.textContent).toBe(expected)
  }
)

test('task log prices use localized unit labels from pricing metadata', async () => {
  client.setQueryData(['pricing'], {
    data: [
      {
        model_name: 'wan2.5-i2v-preview',
        billing_usage_schema: {
          images: {
            type: 'number',
            unit: 'count',
            unitLabel: { en: 'image', zh: '张' },
          },
        },
      },
    ],
    vendors: [],
  })
  const preview = renderPreview({
    is_task: true,
    billing_mode: 'tiered_expr',
    expr_b64: btoa('tier("images", u("images") * 0.25)'),
    matched_tier: 'images',
  })
  expect(preview).toHaveTextContent('images · images $0.25/image')
  await act(() => i18n.changeLanguage('zh-CN'))
  expect(
    screen.getByRole('button', { name: /images · images/ })
  ).toHaveTextContent('images · images $0.25/张')
})

test('task log prices select the executing provider’s schema', () => {
  client.setQueryData(['pricing'], {
    data: [
      {
        model_name: 'wan2.5-i2v-preview',
        billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
        billing_plugin_variants: [
          {
            plugin_key: 'beta',
            plugin_name: 'Beta',
            billing_expr: 'tier("images", u("images") * 0.25)',
            billing_usage_schema: {
              images: {
                type: 'number',
                unit: 'count',
                unitLabel: { en: 'image' },
              },
            },
          },
        ],
      },
    ],
    vendors: [],
  })
  const preview = renderPreview({
    is_task: true,
    billing_mode: 'tiered_expr',
    expr_b64: btoa('tier("images", u("images") * 0.25)'),
    matched_tier: 'images',
    admin_info: {
      task_plugin: { key: 'beta', name: 'Beta', version: '1.0.0' },
    },
  })
  expect(preview).toHaveTextContent('images · images $0.25/image')
})

test.each(['missing schema', 'unsupported expression', 'unknown tier'])(
  'task pricing with %s shows an explicit unavailable summary',
  (scenario) => {
    if (scenario !== 'missing schema') {
      client.setQueryData(['pricing'], {
        data: [
          {
            model_name: 'wan2.5-i2v-preview',
            billing_usage_schema: { clips: { type: 'number', unit: 'count' } },
          },
        ],
        vendors: [],
      })
    }
    const expression =
      scenario === 'unsupported expression'
        ? 'tier("music", max(u("clips"), 1) * 0.25)'
        : 'tier("music", u("clips") * 0.25)'
    const preview = renderPreview({
      is_task: true,
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expression).toString('base64'),
      matched_tier: scenario === 'unknown tier' ? 'old' : 'music',
    })
    expect(preview.textContent).toBe('Dynamic Pricing · No matching results')
  }
)

const diagnosticBody = {
  data: '{"messages":[{"role":"user","content":"original fixture prompt"}]}',
  encoding: 'utf-8',
  bytes: 74,
  captured_bytes: 74,
  truncated: false,
}
const errorDiagnostics: ErrorDiagnostics = {
  version: 1,
  stage: 'upstream_response',
  error: 'invalid fixture request',
  status: 502,
  error_code: 'fixture_unsupported',
  error_type: 'openai_error',
  elapsed_ms: 842,
  client_request: {
    method: 'POST',
    url: '/v1/responses',
    headers: { Authorization: ['Bearer fixture-owner-only'] },
    body: diagnosticBody,
  },
  client_response: { status: 502, body: { ...diagnosticBody, data: '' } },
  attempts: [
    {
      channel_id: 11,
      channel_name: 'Fixture A',
      model: 'fixture-model',
      elapsed_ms: 210,
      request: {
        method: 'POST',
        url: 'https://fixture-a.example/v1/responses',
        body: diagnosticBody,
      },
      response: {
        status: 429,
        body: { ...diagnosticBody, data: 'fixture rate limit' },
      },
    },
    {
      channel_id: 12,
      channel_name: 'Fixture B',
      model: 'upstream-fixture-model',
      elapsed_ms: 632,
      request: {
        method: 'POST',
        url: 'https://fixture-b.example/v1/chat/completions',
        body: { ...diagnosticBody, data: '{"messages":[]}' },
      },
      response: {
        status: 400,
        headers: { 'X-Request-Id': ['fixture-upstream-id'] },
        body: { ...diagnosticBody, data: 'fixture unsupported messages' },
      },
      upstream_request_id: 'fixture-upstream-id',
    },
  ],
}
function renderErrorDialog(
  isRoot = true,
  diagnostics: ErrorDiagnostics | null = errorDiagnostics
) {
  const log = {
    ...makeLog({
      error_code: 'fixture_unsupported',
      status_code: 502,
      root_info: { error_diagnostics: diagnostics ?? undefined },
    }),
    type: 5,
    content: 'legacy fixture summary',
  }
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <DetailsDialog
          log={log}
          isAdmin
          isRoot={isRoot}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

test('root error details show original data and allow switching between upstream attempts', async () => {
  renderErrorDialog()
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.getByText('Error diagnostics')).toBeVisible()
  expect(dialog.getByText('fixture unsupported messages')).toBeVisible()
  expect(
    dialog.getByText('POST https://fixture-b.example/v1/chat/completions')
  ).toBeVisible()
  fireEvent.click(dialog.getByRole('tab', { name: 'Attempt 1' }))
  expect(dialog.getByText('fixture rate limit')).toBeVisible()
  fireEvent.click(dialog.getByRole('tab', { name: 'Original request' }))
  expect(dialog.getByText(/Bearer fixture-owner-only/)).toBeVisible()
  expect(dialog.getByText(/original fixture prompt/)).toBeVisible()
  expect(
    dialog.getByRole('button', { name: 'Copy diagnostic report' })
  ).toBeVisible()
})

test('non-root log details hide raw diagnostics even when a payload contains root metadata', async () => {
  renderErrorDialog(false)
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.queryByText('Error diagnostics')).not.toBeInTheDocument()
  expect(dialog.queryByText(/fixture-owner-only/)).not.toBeInTheDocument()
  expect(dialog.getByText('legacy fixture summary')).toBeVisible()
})

test('truncated diagnostic bodies show an explicit capture limit message', async () => {
  renderErrorDialog(true, {
    ...errorDiagnostics,
    attempts: [
      {
        ...errorDiagnostics.attempts[1],
        response: {
          ...errorDiagnostics.attempts[1].response,
          body: {
            ...diagnosticBody,
            data: 'captured fixture prefix',
            bytes: 100000,
            captured_bytes: 23,
            truncated: true,
          },
        },
      },
    ],
  })
  expect(await screen.findByText('Capture limit reached')).toBeVisible()
  expect(screen.getByText('captured fixture prefix')).toBeVisible()
})

test('legacy error logs remain readable and explain when detailed capture is absent', async () => {
  renderErrorDialog(true, null)
  expect(
    await screen.findByText('No detailed capture is stored for this entry.')
  ).toBeVisible()
  expect(screen.getByText('legacy fixture summary')).toBeVisible()
})
