<script setup lang="ts">
  // Octopus connection form. The API key lives in component memory for the
  // session; persisting it to browser storage is opt-in only, behind an
  // explicit warning. The key is sent to our backend once per calculation
  // and discarded there.
  import { onMounted, ref } from 'vue'

  const emit = defineEmits<{
    submit: [
      details: {
        apiKey: string
        account: string
        periodFrom: string
        periodTo: string
        gasUnit: 'kwh' | 'm3'
      },
    ]
    prefill: [details: { apiKey: string; account: string }]
    connectOctopus: []
    disconnect: []
  }>()

  const props = defineProps<{
    busy: boolean
    /** Whether the server has an OAuth client configured. */
    oauthEnabled: boolean
    /** True when the user completed the OAuth connect flow. */
    oauthConnected: boolean
  }>()

  const KEY_STORAGE = 'ukeb.octopus.key'

  const apiKey = ref('')
  const account = ref('')
  const rememberKey = ref(false)
  const gasUnit = ref<'kwh' | 'm3'>('kwh')

  // Default to the last full year ending yesterday-ish; the user can adjust.
  const today = new Date()
  const iso = (d: Date): string => d.toISOString().slice(0, 10)
  const periodTo = ref(iso(today))
  const periodFrom = ref(iso(new Date(today.getTime() - 365 * 24 * 3600 * 1000)))

  onMounted(() => {
    const saved = localStorage.getItem(KEY_STORAGE)
    if (saved) {
      apiKey.value = saved
      rememberKey.value = true
    }
  })

  // Keys arrive via copy-paste; stray whitespace would corrupt the Basic
  // auth upstream and read back as a baffling 401.
  function cleanKey(): string {
    return apiKey.value.trim()
  }

  function onRememberToggled(): void {
    if (rememberKey.value && cleanKey()) {
      localStorage.setItem(KEY_STORAGE, cleanKey())
    } else {
      localStorage.removeItem(KEY_STORAGE)
    }
  }

  function prefill(): void {
    if (rememberKey.value && cleanKey()) {
      localStorage.setItem(KEY_STORAGE, cleanKey())
    }
    emit('prefill', { apiKey: cleanKey(), account: account.value.trim() })
  }

  function submit(): void {
    if (!props.oauthConnected && rememberKey.value && cleanKey()) {
      localStorage.setItem(KEY_STORAGE, cleanKey())
    }
    emit('submit', {
      apiKey: cleanKey(),
      account: account.value.trim(),
      periodFrom: periodFrom.value,
      periodTo: periodTo.value,
      gasUnit: gasUnit.value,
    })
  }
</script>

<template>
  <form class="space-y-3" @submit.prevent="submit">
    <p class="text-sm text-gray-500">
      Your credentials are sent to this app's backend once per calculation, used to fetch your usage
      from Octopus, and immediately discarded — they are never stored or logged server-side.
    </p>

    <div v-if="oauthEnabled" class="flex items-center gap-3">
      <button
        v-if="!oauthConnected"
        type="button"
        :disabled="busy"
        class="rounded-lg bg-pink-600 px-4 py-2 text-sm font-medium text-white hover:bg-pink-700 disabled:opacity-50"
        @click="emit('connectOctopus')"
      >
        Connect with Octopus (no API key needed)
      </button>
      <template v-else>
        <span class="text-sm text-emerald-700" role="status">Connected to Octopus ✓</span>
        <button
          type="button"
          class="text-sm text-gray-500 hover:text-gray-700 underline"
          @click="emit('disconnect')"
        >
          Disconnect
        </button>
      </template>
    </div>

    <div class="grid sm:grid-cols-2 gap-3">
      <div v-if="!oauthConnected">
        <label for="octo-key" class="block text-sm font-medium text-gray-700">
          Octopus API key
          <a
            href="https://octopus.energy/dashboard/developer/"
            target="_blank"
            rel="noopener noreferrer"
            class="ml-2 font-normal text-indigo-600 hover:text-indigo-800 underline"
          >
            find it here ↗
          </a>
          <input
            id="octo-key"
            v-model="apiKey"
            type="password"
            :required="!oauthConnected"
            autocomplete="off"
            placeholder="sk_live_…"
            class="mt-1 w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm font-mono"
          />
        </label>
      </div>
      <div>
        <label for="octo-account" class="block text-sm font-medium text-gray-700">
          Account number
          <input
            id="octo-account"
            v-model="account"
            type="text"
            required
            placeholder="A-1234ABCD"
            class="mt-1 w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm font-mono"
          />
        </label>
      </div>
      <div>
        <label for="octo-from" class="block text-sm font-medium text-gray-700">
          From
          <input
            id="octo-from"
            v-model="periodFrom"
            type="date"
            required
            class="mt-1 w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          />
        </label>
      </div>
      <div>
        <label for="octo-to" class="block text-sm font-medium text-gray-700">
          To (exclusive)
          <input
            id="octo-to"
            v-model="periodTo"
            type="date"
            required
            class="mt-1 w-full border border-gray-300 rounded-md px-3 py-1.5 text-sm"
          />
        </label>
      </div>
      <div>
        <label for="octo-gas-unit" class="block text-sm font-medium text-gray-700">
          Gas unit
          <select
            id="octo-gas-unit"
            v-model="gasUnit"
            class="mt-1 border border-gray-300 rounded-md px-2 py-1.5 text-sm"
          >
            <option value="kwh">kWh (SMETS1)</option>
            <option value="m3">m³ (SMETS2, converted)</option>
          </select>
        </label>
      </div>
    </div>

    <div v-if="!oauthConnected" class="flex items-start gap-2">
      <label for="octo-remember" class="flex items-start gap-2 text-sm text-gray-600">
        <input
          id="octo-remember"
          v-model="rememberKey"
          type="checkbox"
          class="mt-1 rounded"
          @change="onRememberToggled"
        />
        <span>
          Remember my key in this browser.
          <span class="text-amber-700">
            Warning: anyone with access to this browser profile could read it. Leave unchecked on
            shared machines.
          </span>
        </span>
      </label>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <button
        type="submit"
        :disabled="busy"
        class="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
      >
        {{ busy ? 'Fetching from Octopus…' : 'Fetch usage and calculate' }}
      </button>
      <button
        type="button"
        :disabled="busy || (!oauthConnected && !apiKey.trim()) || !account.trim()"
        class="rounded-lg border border-indigo-300 px-4 py-2 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:opacity-40"
        @click="prefill"
      >
        Prefill my current tariff
      </button>
      <span class="text-xs text-gray-400">
        Adds your live Octopus rates as an editable tariff below.
      </span>
    </div>
  </form>
</template>
