<script setup lang="ts">
  // Save the currently-imported streams under a user-chosen name (IndexedDB)
  // and reload them later, so a re-visit doesn't need the CSVs again. Raw
  // readings stay on the device; this component never touches the network.
  import { onMounted, ref } from 'vue'

  import {
    deleteDataset,
    listDatasets,
    loadDataset,
    saveDataset,
    type DatasetSummary,
    type StoredDataset,
  } from '../lib/datasetStore'

  const props = defineProps<{
    streams: StoredDataset['streams']
    gasUnit: 'kwh' | 'm3'
    tz: string
    hasData: boolean
  }>()

  const emit = defineEmits<{ loaded: [dataset: StoredDataset] }>()

  const datasets = ref<DatasetSummary[]>([])
  const name = ref('')
  const message = ref('')

  async function refresh(): Promise<void> {
    datasets.value = await listDatasets()
  }

  onMounted(refresh)

  async function save(): Promise<void> {
    if (!name.value.trim()) return
    await saveDataset({
      name: name.value.trim(),
      createdAt: Date.now(),
      streams: props.streams,
      gasUnit: props.gasUnit,
      tz: props.tz,
    })
    message.value = `Saved "${name.value.trim()}".`
    await refresh()
  }

  async function load(datasetName: string): Promise<void> {
    const ds = await loadDataset(datasetName)
    if (ds) {
      emit('loaded', ds)
      message.value = `Loaded "${datasetName}".`
    }
  }

  async function remove(datasetName: string): Promise<void> {
    await deleteDataset(datasetName)
    message.value = `Deleted "${datasetName}".`
    await refresh()
  }
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-end gap-2">
      <div class="grow max-w-xs">
        <label for="dataset-name" class="block text-sm font-medium text-gray-700">
          Save imported data as
          <input
            id="dataset-name"
            v-model="name"
            type="text"
            placeholder="e.g. 2025 full year"
            class="mt-1 w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          />
        </label>
      </div>
      <button
        type="button"
        :disabled="!hasData || !name.trim()"
        class="rounded-lg border border-indigo-300 px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:opacity-40"
        @click="save"
      >
        Save to this browser
      </button>
    </div>

    <p v-if="message" class="text-sm text-emerald-700" role="status">{{ message }}</p>

    <ul v-if="datasets.length" class="divide-y divide-gray-100 border border-gray-200 rounded-lg">
      <li
        v-for="d in datasets"
        :key="d.name"
        class="flex items-center justify-between px-3 py-2 text-sm"
      >
        <span class="text-gray-800">{{ d.name }}</span>
        <span class="flex gap-3">
          <button
            type="button"
            class="text-indigo-600 hover:text-indigo-800 underline"
            @click="load(d.name)"
          >
            Load
          </button>
          <button
            type="button"
            class="text-red-600 hover:text-red-800 underline"
            @click="remove(d.name)"
          >
            Delete
          </button>
        </span>
      </li>
    </ul>
  </div>
</template>
