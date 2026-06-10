<script setup lang="ts">
  // Accessible fallback for every chart: the same numbers as a plain table,
  // behind a disclosure toggle. Screen-reader and keyboard users get the
  // data without relying on the canvas; everyone else can verify exact
  // values.
  import { ref } from 'vue'

  defineProps<{
    caption: string
    columns: string[]
    rows: (string | number)[][]
  }>()

  const open = ref(false)
</script>

<template>
  <div class="mt-2">
    <button
      type="button"
      class="text-sm text-indigo-600 hover:text-indigo-800 underline"
      :aria-expanded="open"
      @click="open = !open"
    >
      {{ open ? 'Hide data table' : 'Show data table' }}
    </button>
    <div v-if="open" class="mt-2 max-h-80 overflow-auto border border-gray-200 rounded-lg">
      <table class="w-full text-sm">
        <caption class="sr-only">
          {{
            caption
          }}
        </caption>
        <thead class="bg-gray-50 sticky top-0">
          <tr>
            <th
              v-for="col in columns"
              :key="col"
              scope="col"
              class="text-left font-medium text-gray-600 px-3 py-2"
            >
              {{ col }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="i" class="border-t border-gray-100">
            <td v-for="(cell, j) in row" :key="j" class="px-3 py-1.5 text-gray-700 tabular-nums">
              {{ cell }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
