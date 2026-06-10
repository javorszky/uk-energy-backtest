<script setup lang="ts">
  // "Delete my data" — wipes everything this app keeps in the browser:
  // tariffs (localStorage), datasets (IndexedDB), and any remembered Octopus
  // key. Surfaced prominently because it reinforces the privacy story:
  // nothing lives server-side, so this really is everything.
  import {
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogOverlay,
    AlertDialogPortal,
    AlertDialogRoot,
    AlertDialogTitle,
    AlertDialogTrigger,
  } from 'reka-ui'
  import { ref } from 'vue'

  import { clearDatasets } from '../lib/datasetStore'
  import { clearTariffs } from '../lib/tariffStore'

  const emit = defineEmits<{ deleted: [] }>()

  const done = ref(false)

  async function wipeEverything(): Promise<void> {
    clearTariffs()
    localStorage.removeItem('ukeb.octopus.key')
    await clearDatasets()
    done.value = true
    emit('deleted')
  }
</script>

<template>
  <div class="flex items-center gap-3">
    <AlertDialogRoot>
      <AlertDialogTrigger
        class="rounded-lg border border-red-300 px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50"
      >
        Delete my data
      </AlertDialogTrigger>
      <AlertDialogPortal>
        <AlertDialogOverlay class="fixed inset-0 bg-black/40" />
        <AlertDialogContent
          class="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-md rounded-2xl bg-white p-6 shadow-xl"
        >
          <AlertDialogTitle class="text-lg font-semibold text-gray-900">
            Delete everything stored in this browser?
          </AlertDialogTitle>
          <AlertDialogDescription class="mt-2 text-sm text-gray-600">
            This removes your saved tariffs, all imported usage datasets, and any remembered Octopus
            API key. Nothing is stored on the server, so this wipes all your data from this app.
            This cannot be undone.
          </AlertDialogDescription>
          <div class="mt-5 flex justify-end gap-3">
            <AlertDialogCancel
              class="rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
            >
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              class="rounded-lg bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-700"
              @click="wipeEverything"
            >
              Delete everything
            </AlertDialogAction>
          </div>
        </AlertDialogContent>
      </AlertDialogPortal>
    </AlertDialogRoot>
    <p v-if="done" class="text-sm text-emerald-700" role="status">All local data deleted.</p>
  </div>
</template>
