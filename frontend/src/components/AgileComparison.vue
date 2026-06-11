<script setup lang="ts">
  // Optional historical Agile backtest. Dynamic pricing can't be expressed
  // as a 48-bucket profile, so this costs the FULL raw reading history —
  // which lives only in this browser — against the published historical
  // price series the backend relays. Privacy unchanged: prices flow in,
  // usage flows nowhere.
  import { computed, ref } from 'vue'

  import { getAgileRates, ApiError } from '../api/client'
  import {
    costAgainstRates,
    distinctLocalDates,
    readingsDateRange,
    standingForDays,
  } from '../lib/agile'
  import { pounds } from '../lib/echarts'
  import type { RawReading } from '../lib/types'

  const props = defineProps<{
    importReadings: RawReading[]
    exportReadings?: RawReading[]
  }>()

  // GSP regions (I and O unused). The region letter is the last character of
  // any of the user's electricity tariff codes.
  const regions = [
    { code: 'A', label: 'A — East England' },
    { code: 'B', label: 'B — East Midlands' },
    { code: 'C', label: 'C — London' },
    { code: 'D', label: 'D — Merseyside & North Wales' },
    { code: 'E', label: 'E — West Midlands' },
    { code: 'F', label: 'F — North East England' },
    { code: 'G', label: 'G — North West England' },
    { code: 'H', label: 'H — Southern England' },
    { code: 'J', label: 'J — South East England' },
    { code: 'K', label: 'K — South Wales' },
    { code: 'L', label: 'L — South West England' },
    { code: 'M', label: 'M — Yorkshire' },
    { code: 'N', label: 'N — South Scotland' },
    { code: 'P', label: 'P — North Scotland' },
  ]

  const region = ref('C')
  const importProduct = ref('AGILE-24-10-01')
  const includeExport = ref(false)
  const exportProduct = ref('AGILE-OUTGOING-19-05-13')

  const busy = ref(false)
  const errorMessage = ref('')
  const warnings = ref<string[]>([])

  interface AgileResult {
    importP: number
    standingP: number
    exportCreditP: number
    netP: number
    days: number
    matchedKwh: number
  }
  const result = ref<AgileResult | null>(null)

  const hasExportData = computed(() => (props.exportReadings?.length ?? 0) > 0)

  async function calculate(): Promise<void> {
    errorMessage.value = ''
    warnings.value = []
    result.value = null

    const range = readingsDateRange(props.importReadings)
    if (!range) {
      errorMessage.value =
        'No import readings on this device — upload a CSV or load a dataset first.'
      return
    }

    busy.value = true
    try {
      const common = { region: region.value, periodFrom: range.from, periodTo: range.to }
      const [unitRates, standingRates] = await Promise.all([
        getAgileRates({ ...common, product: importProduct.value, kind: 'unit' }),
        getAgileRates({ ...common, product: importProduct.value, kind: 'standing' }),
      ])

      const importCost = costAgainstRates(props.importReadings, unitRates.results)
      if (importCost.unmatchedCount > 0) {
        warnings.value.push(
          `${importCost.unmatchedCount} import readings (${importCost.unmatchedKwh.toFixed(1)} kWh) ` +
            `fall outside ${importProduct.value}'s published prices and were excluded — ` +
            `an older Agile product code may cover more of your data.`,
        )
      }

      const days = distinctLocalDates(props.importReadings)
      const standing = standingForDays(days, standingRates.results)
      if (standing.uncoveredDays > 0) {
        warnings.value.push(
          `${standing.uncoveredDays} of ${days.length} days had no published standing charge and were excluded.`,
        )
      }

      let exportCreditP = 0
      if (includeExport.value && hasExportData.value) {
        const exportRates = await getAgileRates({
          ...common,
          product: exportProduct.value,
          kind: 'unit',
        })
        const exportCost = costAgainstRates(props.exportReadings ?? [], exportRates.results)
        exportCreditP = exportCost.costP
        if (exportCost.unmatchedCount > 0) {
          warnings.value.push(
            `${exportCost.unmatchedCount} export readings fall outside ${exportProduct.value}'s published prices.`,
          )
        }
      }

      result.value = {
        importP: importCost.costP,
        standingP: standing.costP,
        exportCreditP,
        netP: importCost.costP + standing.costP - exportCreditP,
        days: days.length,
        matchedKwh: importCost.matchedKwh,
      }
    } catch (err) {
      errorMessage.value =
        err instanceof ApiError
          ? err.message
          : 'Could not fetch Agile rates — is the backend running?'
    } finally {
      busy.value = false
    }
  }
</script>

<template>
  <div class="space-y-3">
    <p class="text-sm text-gray-500">
      Agile prices change every half hour, so this comparison is computed from your full reading
      history (kept in your browser) against the historical prices Octopus published — date by date,
      not from the daily profile. Gas is not included (Agile is electricity-only).
    </p>

    <div class="flex flex-wrap items-end gap-3">
      <label for="agile-region" class="block text-sm text-gray-600">
        Region
        <select
          id="agile-region"
          v-model="region"
          class="mt-1 block border border-gray-300 rounded-md px-2 py-1.5 text-sm text-gray-800"
        >
          <option v-for="reg in regions" :key="reg.code" :value="reg.code">{{ reg.label }}</option>
        </select>
      </label>
      <label for="agile-product" class="block text-sm text-gray-600">
        Agile product code
        <input
          id="agile-product"
          v-model="importProduct"
          type="text"
          class="mt-1 block w-56 border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono"
        />
      </label>
    </div>
    <p class="text-xs text-gray-400">
      The region letter is the last character of your electricity tariff code. Older data may need
      an older product code (e.g. AGILE-23-12-06, AGILE-FLEX-22-11-25).
    </p>

    <div v-if="hasExportData" class="flex flex-wrap items-end gap-3">
      <label for="agile-include-export" class="flex items-center gap-2 text-sm text-gray-600">
        <input id="agile-include-export" v-model="includeExport" type="checkbox" class="rounded" />
        Include export credit (Agile Outgoing)
      </label>
      <label v-if="includeExport" for="agile-export-product" class="block text-sm text-gray-600">
        Outgoing product code
        <input
          id="agile-export-product"
          v-model="exportProduct"
          type="text"
          class="mt-1 block w-64 border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono"
        />
      </label>
    </div>

    <button
      type="button"
      :disabled="busy || importReadings.length === 0"
      class="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
      @click="calculate"
    >
      {{ busy ? 'Fetching historical prices…' : 'Calculate Agile comparison' }}
    </button>
    <span v-if="importReadings.length === 0" class="ml-3 text-sm text-gray-400">
      Needs raw readings on this device (CSV upload or saved dataset).
    </span>

    <p v-if="errorMessage" class="text-sm text-red-600" role="alert">{{ errorMessage }}</p>
    <ul v-if="warnings.length" class="text-sm text-amber-700 list-disc pl-5">
      <li v-for="(w, i) in warnings" :key="i">{{ w }}</li>
    </ul>

    <div v-if="result" class="rounded-lg border border-gray-200 bg-gray-50 p-4">
      <h4 class="text-sm font-semibold text-gray-900">
        {{ importProduct }} ({{ region }}) — historical backtest
      </h4>
      <dl class="mt-2 grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
        <div>
          <dt class="text-gray-500">Import</dt>
          <dd class="font-medium text-gray-900 tabular-nums">{{ pounds(result.importP) }}</dd>
        </div>
        <div>
          <dt class="text-gray-500">Standing ({{ result.days }} days)</dt>
          <dd class="font-medium text-gray-900 tabular-nums">{{ pounds(result.standingP) }}</dd>
        </div>
        <div>
          <dt class="text-gray-500">Export credit</dt>
          <dd class="font-medium text-emerald-700 tabular-nums">
            −{{ pounds(result.exportCreditP) }}
          </dd>
        </div>
        <div>
          <dt class="text-gray-500">Net</dt>
          <dd class="font-semibold text-gray-900 tabular-nums">{{ pounds(result.netP) }}</dd>
        </div>
      </dl>
      <p class="mt-2 text-xs text-gray-400">
        {{ result.matchedKwh.toFixed(1) }} kWh imported, costed against the real half-hourly prices
        on each historical day. Compare the net against the tariffs above, minus any gas — Agile
        would replace electricity only.
      </p>
    </div>
  </div>
</template>
