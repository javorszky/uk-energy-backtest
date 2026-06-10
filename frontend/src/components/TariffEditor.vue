<script setup lang="ts">
  // Edits one tariff. All rates are VAT-inclusive pence, exactly as a
  // consumer quote shows them. Band boundaries are constrained to :00/:30 by
  // the select options, so the backend's validation can't be tripped from
  // here.
  //
  // The component works on a local deep copy and emits update:modelValue on
  // every change (vue/no-mutating-props). The parent re-keys the editor list
  // when it replaces tariffs wholesale (preset reset), remounting with fresh
  // copies.
  import { reactive } from 'vue'

  import type { Band, Tariff } from '../lib/types'

  const props = defineProps<{
    modelValue: Tariff
    index: number
  }>()

  const emit = defineEmits<{
    'update:modelValue': [tariff: Tariff]
    remove: []
  }>()

  function clone<T>(v: T): T {
    return JSON.parse(JSON.stringify(v)) as T
  }

  const local = reactive(clone(props.modelValue))

  function changed(): void {
    emit('update:modelValue', clone(local))
  }

  // "00:00" … "23:30" — every legal half-hour boundary.
  const timeOptions = Array.from({ length: 48 }, (_, i) => {
    const h = String(Math.floor(i / 2)).padStart(2, '0')
    return `${h}:${i % 2 === 0 ? '00' : '30'}`
  })

  function toggleGas(event: Event): void {
    const on = (event.target as HTMLInputElement).checked
    if (on) {
      local.gas = { standing_charge: 29.6, rate: 6.2 }
    } else {
      delete local.gas
    }
    changed()
  }

  function addBand(bands: Band[]): void {
    bands.push({ from: '02:00', to: '05:00', rate: 10 })
    changed()
  }

  function removeBand(bands: Band[], i: number): void {
    bands.splice(i, 1)
    changed()
  }

  const id = (suffix: string): string => `tariff-${props.index}-${suffix}`
</script>

<template>
  <fieldset class="border border-gray-200 rounded-xl p-4 space-y-4">
    <legend class="sr-only">Tariff {{ local.name }}</legend>

    <div class="flex items-end gap-3">
      <label :for="id('name')" class="grow block text-sm font-medium text-gray-700">
        Tariff name
        <input
          :id="id('name')"
          v-model="local.name"
          type="text"
          class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm font-normal"
          @change="changed"
        />
      </label>
      <button
        type="button"
        class="text-sm text-red-600 hover:text-red-800 underline pb-2 shrink-0"
        @click="emit('remove')"
      >
        Remove tariff
      </button>
    </div>

    <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
      <label :for="id('standing')" class="block text-sm text-gray-600">
        Elec standing (p/day)
        <input
          :id="id('standing')"
          v-model.number="local.electricity.standing_charge"
          type="number"
          step="0.01"
          min="0"
          class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          @change="changed"
        />
      </label>
      <label :for="id('import-default')" class="block text-sm text-gray-600">
        Import default (p/kWh)
        <input
          :id="id('import-default')"
          v-model.number="local.electricity.import_default"
          type="number"
          step="0.01"
          min="0"
          class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          @change="changed"
        />
      </label>
      <label :for="id('export-default')" class="block text-sm text-gray-600">
        Export default (p/kWh)
        <input
          :id="id('export-default')"
          v-model.number="local.electricity.export_default"
          type="number"
          step="0.01"
          min="0"
          class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          @change="changed"
        />
      </label>
    </div>

    <div
      v-for="(bands, kind) in {
        import: local.electricity.import_bands,
        export: local.electricity.export_bands,
      }"
      :key="kind"
    >
      <h4 class="text-sm font-medium text-gray-700 capitalize">{{ kind }} bands</h4>
      <p class="text-xs text-gray-400 mb-1">
        First match wins; a band ending before it starts wraps past midnight.
      </p>
      <div v-for="(band, i) in bands" :key="i" class="flex items-center gap-2 mt-1">
        <label :for="id(`${kind}-band-${i}-from`)">
          <span class="sr-only">{{ kind }} band {{ i + 1 }} from</span>
          <select
            :id="id(`${kind}-band-${i}-from`)"
            v-model="band.from"
            class="border border-gray-300 rounded-md px-2 py-1 text-sm"
            @change="changed"
          >
            <option v-for="t in timeOptions" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
        <span class="text-sm text-gray-500" aria-hidden="true">→</span>
        <label :for="id(`${kind}-band-${i}-to`)">
          <span class="sr-only">{{ kind }} band {{ i + 1 }} to</span>
          <select
            :id="id(`${kind}-band-${i}-to`)"
            v-model="band.to"
            class="border border-gray-300 rounded-md px-2 py-1 text-sm"
            @change="changed"
          >
            <option v-for="t in timeOptions" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
        <label :for="id(`${kind}-band-${i}-rate`)">
          <span class="sr-only">{{ kind }} band {{ i + 1 }} rate</span>
          <input
            :id="id(`${kind}-band-${i}-rate`)"
            v-model.number="band.rate"
            type="number"
            step="0.01"
            class="w-24 border border-gray-300 rounded-md px-2 py-1 text-sm"
            @change="changed"
          />
        </label>
        <span class="text-xs text-gray-400">p/kWh</span>
        <button
          type="button"
          class="text-sm text-red-500 hover:text-red-700"
          :aria-label="`Remove ${kind} band ${i + 1}`"
          @click="removeBand(bands, i)"
        >
          ✕
        </button>
      </div>
      <button
        type="button"
        class="mt-1 text-sm text-indigo-600 hover:text-indigo-800 underline"
        @click="addBand(bands)"
      >
        Add {{ kind }} band
      </button>
    </div>

    <div class="pt-2 border-t border-gray-100">
      <label :for="id('has-gas')" class="flex items-center gap-2 text-sm text-gray-700">
        <input
          :id="id('has-gas')"
          type="checkbox"
          class="rounded"
          :checked="local.gas !== undefined"
          @change="toggleGas"
        />
        Includes gas
      </label>
      <div v-if="local.gas" class="grid grid-cols-2 gap-3 mt-2">
        <label :for="id('gas-standing')" class="block text-sm text-gray-600">
          Gas standing (p/day)
          <input
            :id="id('gas-standing')"
            v-model.number="local.gas.standing_charge"
            type="number"
            step="0.01"
            min="0"
            class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
            @change="changed"
          />
        </label>
        <label :for="id('gas-rate')" class="block text-sm text-gray-600">
          Gas rate (p/kWh)
          <input
            :id="id('gas-rate')"
            v-model.number="local.gas.rate"
            type="number"
            step="0.01"
            min="0"
            class="mt-1 block w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
            @change="changed"
          />
        </label>
      </div>
    </div>
  </fieldset>
</template>
