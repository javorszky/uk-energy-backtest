<script setup lang="ts">
  // The headline answer: one stacked bar per tariff — import, gas, standing
  // stacked positive, export credit negative — with the net labelled.
  import { computed } from 'vue'

  import { pounds, VChart } from '../lib/echarts'
  import type { CostResult } from '../lib/types'

  import ChartDataTable from './ChartDataTable.vue'

  const props = defineProps<{ results: CostResult[] }>()

  const option = computed(() => ({
    tooltip: {
      trigger: 'axis',
      valueFormatter: (v: number) => pounds(v),
    },
    legend: { bottom: 0 },
    grid: { left: 70, right: 20, top: 40, bottom: 60 },
    xAxis: {
      type: 'category',
      data: props.results.map((r) => r.name),
      axisLabel: { interval: 0, width: 120, overflow: 'break' },
    },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: (v: number) => pounds(v) },
    },
    series: [
      {
        name: 'Import',
        type: 'bar',
        stack: 'cost',
        data: props.results.map((r) => r.import_p),
        itemStyle: { color: '#6366f1' },
      },
      {
        name: 'Gas',
        type: 'bar',
        stack: 'cost',
        data: props.results.map((r) => r.gas_p),
        itemStyle: { color: '#f59e0b' },
      },
      {
        name: 'Standing',
        type: 'bar',
        stack: 'cost',
        data: props.results.map((r) => r.standing_p),
        itemStyle: { color: '#94a3b8' },
      },
      {
        name: 'Export credit',
        type: 'bar',
        stack: 'cost',
        data: props.results.map((r) => -r.export_credit_p),
        itemStyle: { color: '#10b981' },
        label: {
          show: true,
          position: 'bottom',
          formatter: ({ dataIndex }: { dataIndex: number }) =>
            `net ${pounds(props.results[dataIndex].net_p)}`,
          color: '#111827',
          fontWeight: 'bold',
        },
      },
    ],
  }))

  const tableRows = computed(() =>
    props.results.map((r) => [
      r.name,
      pounds(r.import_p),
      pounds(r.gas_p),
      pounds(r.standing_p),
      `−${pounds(r.export_credit_p)}`,
      pounds(r.net_p),
    ]),
  )

  const ariaSummary = computed(() =>
    props.results.map((r) => `${r.name}: net ${pounds(r.net_p)}`).join('; '),
  )
</script>

<template>
  <figure>
    <figcaption class="text-base font-semibold text-gray-900 mb-2">
      Cost comparison
      <span class="sr-only">— {{ ariaSummary }}</span>
    </figcaption>
    <VChart
      class="h-80 w-full"
      :option="option"
      autoresize
      role="img"
      :aria-label="`Stacked cost bars per tariff. ${ariaSummary}`"
    />
    <ChartDataTable
      caption="Cost breakdown per tariff"
      :columns="['Tariff', 'Import', 'Gas', 'Standing', 'Export credit', 'Net']"
      :rows="tableRows"
    />
  </figure>
</template>
