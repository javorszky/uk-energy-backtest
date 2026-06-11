<script setup lang="ts">
  // The headline answer, with electricity and gas as separate entities. The
  // electricity chart stacks import + standing positive and export credit
  // negative, labelled with the electricity net — directly comparable to
  // electricity-only offers (and the Agile backtest). Gas, when any tariff
  // prices it, gets its own bars; the table carries the combined totals.
  import { computed } from 'vue'

  import { pounds, VChart } from '../lib/echarts'
  import type { CostResult } from '../lib/types'

  import ChartDataTable from './ChartDataTable.vue'

  const props = defineProps<{ results: CostResult[] }>()

  const elecOption = computed(() => ({
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
        stack: 'elec',
        data: props.results.map((r) => r.import_p),
        itemStyle: { color: '#6366f1' },
      },
      {
        name: 'Standing',
        type: 'bar',
        stack: 'elec',
        data: props.results.map((r) => r.elec_standing_p),
        itemStyle: { color: '#94a3b8' },
      },
      {
        name: 'Export credit',
        type: 'bar',
        stack: 'elec',
        data: props.results.map((r) => -r.export_credit_p),
        itemStyle: { color: '#10b981' },
        label: {
          show: true,
          position: 'bottom',
          formatter: ({ dataIndex }: { dataIndex: number }) =>
            `elec net ${pounds(props.results[dataIndex].elec_net_p)}`,
          color: '#111827',
          fontWeight: 'bold',
        },
      },
    ],
  }))

  const gasResults = computed(() => props.results.filter((r) => r.gas_total_p > 0))
  const hasGas = computed(() => gasResults.value.length > 0)

  const gasOption = computed(() => ({
    tooltip: {
      trigger: 'axis',
      valueFormatter: (v: number) => pounds(v),
    },
    legend: { bottom: 0 },
    grid: { left: 70, right: 20, top: 40, bottom: 60 },
    xAxis: {
      type: 'category',
      data: gasResults.value.map((r) => r.name),
      axisLabel: { interval: 0, width: 120, overflow: 'break' },
    },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: (v: number) => pounds(v) },
    },
    series: [
      {
        name: 'Gas usage',
        type: 'bar',
        stack: 'gas',
        data: gasResults.value.map((r) => r.gas_p),
        itemStyle: { color: '#f59e0b' },
      },
      {
        name: 'Gas standing',
        type: 'bar',
        stack: 'gas',
        data: gasResults.value.map((r) => r.gas_standing_p),
        itemStyle: { color: '#d6d3d1' },
        label: {
          show: true,
          position: 'top',
          formatter: ({ dataIndex }: { dataIndex: number }) =>
            `gas ${pounds(gasResults.value[dataIndex].gas_total_p)}`,
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
      pounds(r.elec_standing_p),
      `−${pounds(r.export_credit_p)}`,
      pounds(r.elec_net_p),
      pounds(r.gas_p),
      pounds(r.gas_standing_p),
      pounds(r.gas_total_p),
      pounds(r.total_p),
    ]),
  )

  const elecAriaSummary = computed(() =>
    props.results.map((r) => `${r.name}: electricity net ${pounds(r.elec_net_p)}`).join('; '),
  )

  const gasAriaSummary = computed(() =>
    gasResults.value.map((r) => `${r.name}: gas total ${pounds(r.gas_total_p)}`).join('; '),
  )
</script>

<template>
  <div class="space-y-6">
    <figure>
      <figcaption class="text-base font-semibold text-gray-900 mb-2">
        Electricity comparison
        <span class="sr-only">— {{ elecAriaSummary }}</span>
      </figcaption>
      <VChart
        class="h-80 w-full"
        :option="elecOption"
        autoresize
        role="img"
        :aria-label="`Stacked electricity cost bars per tariff. ${elecAriaSummary}`"
      />
    </figure>

    <figure v-if="hasGas">
      <figcaption class="text-base font-semibold text-gray-900 mb-2">
        Gas comparison
        <span class="sr-only">— {{ gasAriaSummary }}</span>
        <span class="ml-2 text-xs font-normal text-gray-400">
          Only tariffs that price gas are shown.
        </span>
      </figcaption>
      <VChart
        class="h-64 w-full"
        :option="gasOption"
        autoresize
        role="img"
        :aria-label="`Stacked gas cost bars per tariff. ${gasAriaSummary}`"
      />
    </figure>

    <ChartDataTable
      caption="Cost breakdown per tariff, electricity and gas separately plus the combined total"
      :columns="[
        'Tariff',
        'Import',
        'Elec standing',
        'Export credit',
        'Elec net',
        'Gas usage',
        'Gas standing',
        'Gas total',
        'Combined total',
      ]"
      :rows="tableRows"
    />
  </div>
</template>
