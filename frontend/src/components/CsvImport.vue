<script setup lang="ts">
  // Client-side CSV ingestion for the three streams. Parsing and
  // profile-building happen on the device; the parent receives raw readings
  // and only ever sends the aggregated profile to the backend.
  import Papa from 'papaparse'
  import { reactive, ref } from 'vue'

  import { detectPreset, parseCsv, presets, type ColumnMapping, type CsvPreset } from '../lib/csv'
  import { LONDON_TZ } from '../lib/timezone'
  import type { RawReading } from '../lib/types'

  const emit = defineEmits<{
    parsed: [stream: StreamKind, readings: RawReading[], warnings: string[]]
    gasUnitChanged: [unit: 'kwh' | 'm3']
    timezoneChanged: [tz: string]
  }>()

  type StreamKind = 'import' | 'export' | 'gas'

  const streams: { kind: StreamKind; label: string; hint: string }[] = [
    { kind: 'import', label: 'Electricity import', hint: 'Required for any comparison' },
    { kind: 'export', label: 'Electricity export', hint: 'Solar/battery households' },
    { kind: 'gas', label: 'Gas', hint: 'Optional' },
  ]

  interface StreamState {
    file: File | null
    headers: string[]
    presetId: CsvPreset['id']
    mapping: ColumnMapping | null
    rowCount: number | null
    warnings: string[]
    error: string
  }

  const state = reactive<Record<StreamKind, StreamState>>({
    import: emptyState(),
    export: emptyState(),
    gas: emptyState(),
  })

  function emptyState(): StreamState {
    return {
      file: null,
      headers: [],
      presetId: 'generic',
      mapping: null,
      rowCount: null,
      warnings: [],
      error: '',
    }
  }

  // The timezone the CSVs' bare timestamps are written in. Load-bearing for
  // costing rule 1: a wrong zone shifts peak/off-peak by an hour for half
  // the year. Timestamps with explicit offsets ignore this.
  const sourceTz = ref(LONDON_TZ)
  const tzOptions = [LONDON_TZ, 'UTC']

  const gasUnit = ref<'kwh' | 'm3'>('kwh')

  function readHeaders(file: File): Promise<string[]> {
    return new Promise((resolve, reject) => {
      Papa.parse(file, {
        preview: 1,
        complete: (res) => resolve(((res.data[0] as string[]) ?? []).map((h) => h.trim())),
        error: (err: Error) => reject(new Error(err.message)),
      })
    })
  }

  async function onFileChosen(kind: StreamKind, event: Event): Promise<void> {
    const input = event.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return

    const s = state[kind]
    Object.assign(s, emptyState())
    s.file = file
    try {
      s.headers = await readHeaders(file)
    } catch (err) {
      s.error = `Could not read file: ${err instanceof Error ? err.message : String(err)}`
      return
    }
    const preset = detectPreset(s.headers)
    s.presetId = preset.id
    s.mapping = preset.mapping(s.headers) ?? {
      timestampCol: s.headers[0] ?? '',
      valueCol: s.headers[1] ?? '',
    }
    await parseStream(kind)
  }

  function onPresetChanged(kind: StreamKind): void {
    const s = state[kind]
    const preset = presets.find((p) => p.id === s.presetId)
    if (preset) {
      s.mapping = preset.mapping(s.headers) ?? s.mapping
    }
    void parseStream(kind)
  }

  async function parseStream(kind: StreamKind): Promise<void> {
    const s = state[kind]
    if (!s.file || !s.mapping?.timestampCol || !s.mapping.valueCol) return
    const preset = presets.find((p) => p.id === s.presetId)
    const tz = preset?.forcedTz ?? sourceTz.value
    s.error = ''
    try {
      const { readings, warnings } = await parseCsv(s.file, s.mapping, tz)
      s.rowCount = readings.length
      s.warnings = warnings
      emit('parsed', kind, readings, warnings)
    } catch (err) {
      s.error = err instanceof Error ? err.message : String(err)
    }
  }

  function reparseAll(): void {
    emit('timezoneChanged', sourceTz.value)
    for (const { kind } of streams) {
      void parseStream(kind)
    }
  }
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-end gap-4">
      <div>
        <label for="csv-tz" class="block text-sm font-medium text-gray-700">
          CSV timestamp timezone
          <select
            id="csv-tz"
            v-model="sourceTz"
            class="mt-1 border border-gray-300 rounded-md px-2 py-1.5 text-sm"
            @change="reparseAll"
          >
            <option v-for="tz in tzOptions" :key="tz" :value="tz">{{ tz }}</option>
          </select>
        </label>
        <p class="text-xs text-gray-400 mt-1">
          Ignored for timestamps that carry their own offset (e.g. Octopus exports).
        </p>
      </div>
      <div>
        <label for="csv-gas-unit" class="block text-sm font-medium text-gray-700">
          Gas unit
          <select
            id="csv-gas-unit"
            v-model="gasUnit"
            class="mt-1 border border-gray-300 rounded-md px-2 py-1.5 text-sm"
            @change="emit('gasUnitChanged', gasUnit)"
          >
            <option value="kwh">kWh (SMETS1)</option>
            <option value="m3">m³ (SMETS2, converted)</option>
          </select>
        </label>
      </div>
    </div>

    <div
      v-for="{ kind, label, hint } in streams"
      :key="kind"
      class="border border-gray-200 rounded-xl p-4"
    >
      <div class="flex items-baseline justify-between">
        <h3 class="text-sm font-medium text-gray-800">{{ label }}</h3>
        <span class="text-xs text-gray-400">{{ hint }}</span>
      </div>

      <label :for="`csv-file-${kind}`" class="block">
        <span class="sr-only">{{ label }} CSV file</span>
        <input
          :id="`csv-file-${kind}`"
          type="file"
          accept=".csv,text/csv"
          class="mt-2 block w-full text-sm text-gray-600 file:mr-3 file:rounded-md file:border-0 file:bg-indigo-50 file:px-3 file:py-1.5 file:text-sm file:text-indigo-700 hover:file:bg-indigo-100"
          @change="onFileChosen(kind, $event)"
        />
      </label>

      <div v-if="state[kind].file" class="mt-3 space-y-2">
        <div class="flex flex-wrap gap-3 items-end">
          <label :for="`csv-preset-${kind}`" class="block text-xs text-gray-500">
            Format
            <select
              :id="`csv-preset-${kind}`"
              v-model="state[kind].presetId"
              class="mt-0.5 block border border-gray-300 rounded-md px-2 py-1 text-sm text-gray-800"
              @change="onPresetChanged(kind)"
            >
              <option v-for="p in presets" :key="p.id" :value="p.id">{{ p.label }}</option>
            </select>
          </label>
          <label
            v-if="state[kind].mapping"
            :for="`csv-ts-col-${kind}`"
            class="block text-xs text-gray-500"
          >
            Timestamp column
            <select
              :id="`csv-ts-col-${kind}`"
              v-model="state[kind].mapping!.timestampCol"
              class="mt-0.5 block border border-gray-300 rounded-md px-2 py-1 text-sm text-gray-800"
              @change="parseStream(kind)"
            >
              <option v-for="h in state[kind].headers" :key="h" :value="h">{{ h }}</option>
            </select>
          </label>
          <label
            v-if="state[kind].mapping"
            :for="`csv-val-col-${kind}`"
            class="block text-xs text-gray-500"
          >
            {{ kind === 'gas' ? 'Consumption column' : 'kWh column' }}
            <select
              :id="`csv-val-col-${kind}`"
              v-model="state[kind].mapping!.valueCol"
              class="mt-0.5 block border border-gray-300 rounded-md px-2 py-1 text-sm text-gray-800"
              @change="parseStream(kind)"
            >
              <option v-for="h in state[kind].headers" :key="h" :value="h">{{ h }}</option>
            </select>
          </label>
        </div>

        <p v-if="state[kind].rowCount !== null" class="text-sm text-gray-600">
          {{ state[kind].rowCount }} readings parsed.
        </p>
        <p v-if="state[kind].error" class="text-sm text-red-600" role="alert">
          {{ state[kind].error }}
        </p>
        <ul v-if="state[kind].warnings.length" class="text-sm text-amber-700 list-disc pl-5">
          <li v-for="(w, i) in state[kind].warnings" :key="i">{{ w }}</li>
        </ul>
      </div>
    </div>
  </div>
</template>
