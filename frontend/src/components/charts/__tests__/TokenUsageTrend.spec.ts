import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { Chart as ChartJS, type Chart } from 'chart.js'

import TokenUsageTrend from '../TokenUsageTrend.vue'

const messages: Record<string, string> = {
  'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    name: 'Line',
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

enableAutoUnmount(afterEach)

const crossingTrend = [
  { date: '2026-05-08', cache_creation_tokens: 10, cache_read_tokens: 90 },
  { date: '2026-05-09', cache_creation_tokens: 90, cache_read_tokens: 10 },
].map((point) => ({
  ...point, requests: 1, input_tokens: 20, output_tokens: 5, cost: 0.02, actual_cost: 0.01,
}))

describe('TokenUsageTrend', () => {
  let originalRootClass: string
  let themeStyle: HTMLStyleElement

  beforeEach(() => {
    originalRootClass = document.documentElement.className
    document.documentElement.classList.remove('dark')
    themeStyle = document.createElement('style')
    // Distinct custom tokens prove cssVar is reread, rather than just its fallback changing.
    themeStyle.textContent = `
      :root { --chart-line: #112233; --chart-fill: #223344; --chart-muted: #334455; }
      :root.dark { --chart-line: #ddeeff; --chart-fill: #ccddee; --chart-muted: #bbccdd; }
    `
    document.head.appendChild(themeStyle)
  })

  afterEach(() => {
    themeStyle.remove()
    document.documentElement.className = originalRootClass
    vi.restoreAllMocks()
  })

  it.each([false, true])('distinguishes crossing cache series and their legend markers (dark=%s)', (dark) => {
    document.documentElement.classList.toggle('dark', dark)
    const wrapper = mount(TokenUsageTrend, { props: { trendData: crossingTrend } })
    const line = wrapper.findComponent({ name: 'Line' })
    const { datasets } = line.props('data')
    const creation = datasets.find((dataset: any) => dataset.label === 'Cache Creation')
    const read = datasets.find((dataset: any) => dataset.label === 'Cache Read')
    expect(creation.data).toEqual([10, 90])
    expect(read.data).toEqual([90, 10])
    expect(creation.borderDash ?? []).not.toEqual(read.borderDash ?? [])
    expect(creation.pointStyle ?? 'circle').not.toEqual(read.pointStyle ?? 'circle')

    // Exercise Chart.js's real legend generator with the component's options.
    // Only canvas/controller metadata is substituted; a global pointStyle override
    // would collapse these markers and make this assertion fail.
    const chart = {
      data: { datasets },
      legend: { options: line.props('options').plugins.legend },
      _getSortedDatasetMetas: () => datasets.map((dataset: any, index: number) => ({
        index, visible: true,
        controller: { getStyle: () => ({ pointStyle: 'circle', ...dataset }) },
      })),
    } as unknown as Chart
    const items = ChartJS.defaults.plugins.legend.labels.generateLabels(chart)
    expect(items.find((item) => item.text === 'Cache Creation')?.pointStyle)
      .not.toEqual(items.find((item) => item.text === 'Cache Read')?.pointStyle)
  })

  it.each([false, true])('refreshes mounted chart colors in both theme directions (initial dark=%s)', async (dark) => {
    document.documentElement.classList.toggle('dark', dark)
    const wrapper = mount(TokenUsageTrend, { props: { trendData: crossingTrend } })
    const line = wrapper.findComponent({ name: 'Line' })
    const instance = line.vm
    const colors = () => {
      const { datasets } = line.props('data')
      const options = line.props('options')
      return {
        line: datasets[0].borderColor, fill: datasets[0].backgroundColor,
        point: datasets[0].pointBackgroundColor, muted: datasets[1].borderColor,
        faint: datasets[2].borderColor, cacheRead: datasets[3].borderColor,
        hitRate: datasets[4].borderColor,
        legend: options.plugins.legend.labels.color,
        xGrid: options.scales.x.grid.color, yGrid: options.scales.y.grid.color,
        xText: options.scales.x.ticks.color, yText: options.scales.y.ticks.color,
        percentText: options.scales.yPercent.ticks.color,
      }
    }
    const initial = colors()
    expect(initial.line).toBe(dark ? '#ddeeff' : '#112233')
    document.documentElement.classList.toggle('dark', !dark)
    await flushPromises()
    const changed = colors()
    expect(changed.line).toBe(dark ? '#112233' : '#ddeeff')
    for (const key of Object.keys(initial) as (keyof typeof initial)[]) {
      expect(changed[key], key).not.toBe(initial[key])
    }
    expect(wrapper.findComponent({ name: 'Line' }).vm).toBe(instance)
    expect(line.props('data').datasets[2].data).toEqual([10, 90])
    document.documentElement.classList.toggle('dark', dark)
    await flushPromises()
    expect(colors()).toEqual(initial)
    expect(wrapper.findComponent({ name: 'Line' }).vm).toBe(instance)
  })

  it('disconnects its root theme observer when unmounted', () => {
    const observe = vi.spyOn(MutationObserver.prototype, 'observe')
    const disconnect = vi.spyOn(MutationObserver.prototype, 'disconnect')
    const wrapper = mount(TokenUsageTrend, { props: { trendData: crossingTrend } })
    expect(observe).toHaveBeenCalledWith(document.documentElement, {
      attributes: true, attributeFilter: ['class'],
    })
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })

  it('calculates cache hit rate against all prompt tokens', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 500,
            output_tokens: 100,
            cache_creation_tokens: 0,
            cache_read_tokens: 1500,
            cost: 0.01,
            actual_cost: 0.005,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    // Hit rate = 1500 / (500 + 1500 + 0) * 100 = 75%
    expect(hitRateDataset.data[0]).toBe(75)
  })

  it('returns 0 hit rate when all prompt tokens are zero', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 0,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            cost: 0,
            actual_cost: 0,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    expect(hitRateDataset.data[0]).toBe(0)
  })

  it('includes cache_creation_tokens in denominator for Anthropic models', () => {
    const wrapper = mount(TokenUsageTrend, {
      props: {
        trendData: [
          {
            date: '2026-05-08',
            requests: 1,
            input_tokens: 200,
            output_tokens: 50,
            cache_creation_tokens: 300,
            cache_read_tokens: 500,
            cost: 0.02,
            actual_cost: 0.01,
          },
        ],
      },
      global: {
        stubs: {
          LoadingSpinner: true,
        },
      },
    })

    const chartData = JSON.parse(wrapper.find('.chart-data').text())
    const hitRateDataset = chartData.datasets.find(
      (ds: any) => ds.label === 'Cache Hit Rate'
    )
    // Hit rate = 500 / (200 + 500 + 300) * 100 = 50%
    expect(hitRateDataset.data[0]).toBe(50)
  })
})
