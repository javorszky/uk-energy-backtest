<script setup lang="ts">
  // Daily load profile vs rate bands: the profile's 48 buckets as bars, the
  // selected tariff's resolved per-bucket rates overlaid as a step line on a
  // second axis. This chart explains the comparison — it shows *where* usage
  // lands relative to peak/off-peak windows.
  import { computed, ref } from 'vue'

  import { BUCKET_LABELS, VChart } from '../lib/echarts'
  import type { CostResult, Profile } from '../lib/types'

  import ChartDataTable from './ChartDataTable.vue'

  const props = defineProps<{
    profile: Profile
    results: CostResult[]
  }>()

  const selectedTariff = ref(0)

  const selected = computed<CostResult | undefined>(() => props.results[selectedTariff.value])

  const hasExport = computed(() => props.profile.export_hh !== undefined)

  const option = computed(() => {
    const series: object[] = [
      {
        name: 'Import kWh',
        type: 'bar',
        data: props.profile.import_hh,
        itemStyle: { color: '#6366f1' },
      },
    ]
    if (props.profile.export_hh) {
      series.push({
        name: 'Export kWh',
        type: 'bar',
        data: props.profile.export_hh.map((v) => -v),
        itemStyle: { color: '#10b981' },
      })
    }
    if (selected.value) {
      series.push({
        name: 'Import rate (p/kWh)',
        type: 'line',
        step: 'end',
        symbol: 'none',
        yAxisIndex: 1,
        data: selected.value.import_rates,
        lineStyle: { color: '#dc2626', width: 2, type: 'dashed' },
        itemStyle: { color: '#dc2626' },
      })
      if (hasExport.value) {
        series.push({
          name: 'Export rate (p/kWh)',
          type: 'line',
          step: 'end',
          symbol: 'none',
          yAxisIndex: 1,
          data: selected.value.export_rates,
          lineStyle: { color: '#059669', width: 2, type: 'dotted' },
          itemStyle: { color: '#059669' },
        })
      }
    }
    return {
      tooltip: { trigger: 'axis' },
      legend: { bottom: 0 },
      grid: { left: 60, right: 60, top: 30, bottom: 60 },
      xAxis: {
        type: 'category',
        data: BUCKET_LABELS,
        name: 'Local time of day',
        nameLocation: 'middle',
        nameGap: 28,
      },
      yAxis: [
        { type: 'value', name: 'kWh' },
        { type: 'value', name: 'p/kWh' },
      ],
      series,
    }
  })

  const tableColumns = computed(() => {
    const cols = ['Local half-hour', 'Import kWh']
    if (hasExport.value) cols.push('Export kWh')
    if (selected.value) cols.push('Import rate p/kWh')
    if (selected.value && hasExport.value) cols.push('Export rate p/kWh')
    return cols
  })

  const tableRows = computed(() =>
    BUCKET_LABELS.map((label, i) => {
      const row: (string | number)[] = [label, props.profile.import_hh[i].toFixed(2)]
      if (props.profile.export_hh) row.push(props.profile.export_hh[i].toFixed(2))
      if (selected.value) row.push(selected.value.import_rates[i])
      if (selected.value && props.profile.export_hh) row.push(selected.value.export_rates[i])
      return row
    }),
  )

  const totalImport = computed(() => props.profile.import_hh.reduce((a, b) => a + b, 0).toFixed(1))
</script>

<template>
  <figure>
    <figcaption class="text-base font-semibold text-gray-900 mb-2">
      Daily load profile vs rate bands
    </figcaption>

    <div class="mb-2">
      <label for="overlay-tariff" class="text-sm text-gray-600 mr-2">
        Overlay tariff
        <select
          id="overlay-tariff"
          v-model.number="selectedTariff"
          class="border border-gray-300 rounded-md px-2 py-1 text-sm"
        >
          <option v-for="(r, i) in results" :key="r.name" :value="i">{{ r.name }}</option>
        </select>
      </label>
    </div>

    <VChart
      class="h-80 w-full"
      :option="option"
      autoresize
      role="img"
      :aria-label="`Average daily usage by local half-hour, ${totalImport} kWh imported in total, with ${selected?.name ?? 'no'} tariff rates overlaid. Full numbers in the data table below.`"
    />
    <ChartDataTable
      caption="Usage and rate per local half-hour"
      :columns="tableColumns"
      :rows="tableRows"
    />
  </figure>
</template>
