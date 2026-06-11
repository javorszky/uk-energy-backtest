<script setup lang="ts">
  // Single-page flow: 1) get usage data (CSV parsed locally, or Octopus via
  // the backend proxy), 2) define tariffs, 3) calculate and compare. The
  // privacy invariant lives here: only buildProfile() output or Octopus
  // connection details ever reach the API layer — raw readings do not.
  import { computed, onMounted, reactive, ref, watch } from 'vue'

  import {
    ApiError,
    getOAuthConfig,
    postCost,
    postOAuthToken,
    postOctopusCost,
    postOctopusTariff,
    type OctopusAuth,
  } from './api/client'
  import {
    buildAuthorizeRedirect,
    consumeCallback,
    redirectUri,
    OAUTH_CALLBACK_PATH,
    type OAuthConfig,
  } from './lib/oauth'
  import { buildProfile } from './lib/profile'
  import { loadTariffs, presetTariffs, saveTariffs } from './lib/tariffStore'
  import { LONDON_TZ } from './lib/timezone'
  import type { CostResult, Profile, RawReading, Tariff } from './lib/types'
  import type { StoredDataset } from './lib/datasetStore'

  import CostComparisonChart from './components/CostComparisonChart.vue'
  import CsvImport from './components/CsvImport.vue'
  import DatasetManager from './components/DatasetManager.vue'
  import DeleteMyData from './components/DeleteMyData.vue'
  import LoadProfileChart from './components/LoadProfileChart.vue'
  import OctopusConnect from './components/OctopusConnect.vue'
  import TariffEditor from './components/TariffEditor.vue'

  // --- usage data state (device-local) ---
  const streams = reactive<StoredDataset['streams']>({})
  const gasUnit = ref<'kwh' | 'm3'>('kwh')
  const sourceTz = ref(LONDON_TZ)
  const dataWarnings = ref<string[]>([])

  const hasImportData = computed(() => (streams.import?.length ?? 0) > 0)

  function onCsvParsed(kind: 'import' | 'export' | 'gas', readings: RawReading[]): void {
    streams[kind] = readings
  }

  function onDatasetLoaded(ds: StoredDataset): void {
    streams.import = ds.streams.import
    streams.export = ds.streams.export
    streams.gas = ds.streams.gas
    gasUnit.value = ds.gasUnit
    sourceTz.value = ds.tz
  }

  function onAllDataDeleted(): void {
    streams.import = undefined
    streams.export = undefined
    streams.gas = undefined
    tariffs.value = presetTariffs()
    tariffsVersion.value++
    profile.value = null
    results.value = null
  }

  // --- tariffs (localStorage-backed) ---
  const tariffs = ref<Tariff[]>(loadTariffs())
  watch(tariffs, (t) => saveTariffs(t), { deep: true })

  // TariffEditor keeps a local copy, so when the list is restructured
  // (add/remove/reset) the editors must remount with fresh copies — bumping
  // this version changes their keys.
  const tariffsVersion = ref(0)

  function addTariff(): void {
    tariffs.value.push({
      name: `Tariff ${tariffs.value.length + 1}`,
      electricity: {
        standing_charge: 50,
        import_default: 25,
        import_bands: [],
        export_default: 15,
        export_bands: [],
      },
    })
    tariffsVersion.value++
  }

  function removeTariff(i: number): void {
    tariffs.value.splice(i, 1)
    tariffsVersion.value++
  }

  function resetToPresets(): void {
    tariffs.value = presetTariffs()
    tariffsVersion.value++
  }

  // --- Octopus OAuth (enabled only when the server has a client id) ---
  const oauthConfig = ref<OAuthConfig>({ enabled: false })
  // Access token lives in memory only; a page reload means reconnecting.
  const oauthToken = ref('')

  onMounted(async () => {
    try {
      oauthConfig.value = await getOAuthConfig()
    } catch {
      // Backend unreachable — the health story is told elsewhere; OAuth
      // simply stays hidden.
    }
    if (window.location.pathname === OAUTH_CALLBACK_PATH) {
      await completeOAuthCallback()
    }
  })

  async function startOAuthConnect(): Promise<void> {
    errorMessage.value = ''
    try {
      window.location.assign(await buildAuthorizeRedirect(oauthConfig.value))
    } catch (err) {
      errorMessage.value = err instanceof Error ? err.message : String(err)
    }
  }

  async function completeOAuthCallback(): Promise<void> {
    const params = consumeCallback(new URLSearchParams(window.location.search))
    // Clean the URL either way so a reload doesn't replay the callback.
    window.history.replaceState(null, '', '/')
    if (!params) {
      errorMessage.value = 'Octopus sign-in failed: invalid or expired callback. Please try again.'
      return
    }
    try {
      const tokens = await postOAuthToken({
        grant_type: 'authorization_code',
        code: params.code,
        code_verifier: params.codeVerifier,
        redirect_uri: redirectUri(),
      })
      oauthToken.value = tokens.access_token
    } catch (err) {
      errorMessage.value =
        err instanceof ApiError ? err.message : 'Octopus sign-in failed during token exchange.'
    }
  }

  /** Token wins over a pasted key when both exist. */
  function octopusAuth(apiKey: string): OctopusAuth {
    return oauthToken.value
      ? { kind: 'token', value: oauthToken.value }
      : { kind: 'key', value: apiKey }
  }

  // --- calculation ---
  const profile = ref<Profile | null>(null)
  const results = ref<CostResult[] | null>(null)
  const busy = ref(false)
  const errorMessage = ref('')

  async function calculateFromCsv(): Promise<void> {
    errorMessage.value = ''
    busy.value = true
    try {
      const built = buildProfile({
        importReadings: streams.import ?? [],
        exportReadings: streams.export,
        gasReadings: streams.gas,
        gasUnit: gasUnit.value,
      })
      dataWarnings.value = built.warnings
      const resp = await postCost(built.profile, tariffs.value)
      profile.value = built.profile
      results.value = resp.results
    } catch (err) {
      errorMessage.value =
        err instanceof ApiError ? err.message : 'Calculation failed — is the backend running?'
    } finally {
      busy.value = false
    }
  }

  async function prefillTariffFromOctopus(details: {
    apiKey: string
    account: string
  }): Promise<void> {
    errorMessage.value = ''
    busy.value = true
    try {
      const resp = await postOctopusTariff(details.account, octopusAuth(details.apiKey))
      tariffs.value.push(resp.tariff)
      tariffsVersion.value++
      dataWarnings.value = resp.warnings
    } catch (err) {
      errorMessage.value =
        err instanceof ApiError ? err.message : 'Tariff prefill failed — is the backend running?'
    } finally {
      busy.value = false
    }
  }

  async function calculateFromOctopus(details: {
    apiKey: string
    account: string
    periodFrom: string
    periodTo: string
    gasUnit: 'kwh' | 'm3'
  }): Promise<void> {
    errorMessage.value = ''
    busy.value = true
    try {
      const resp = await postOctopusCost(
        {
          account: details.account,
          period_from: details.periodFrom,
          period_to: details.periodTo,
          gas_unit: details.gasUnit,
          tariffs: tariffs.value,
        },
        octopusAuth(details.apiKey),
      )
      profile.value = resp.profile
      results.value = resp.results
      dataWarnings.value = []
    } catch (err) {
      errorMessage.value =
        err instanceof ApiError ? err.message : 'Octopus fetch failed — is the backend running?'
    } finally {
      busy.value = false
    }
  }
</script>

<template>
  <div class="min-h-screen bg-gray-50">
    <main class="mx-auto max-w-4xl px-4 py-8 space-y-10">
      <header>
        <h1 class="text-2xl font-semibold text-gray-900 tracking-tight">
          Energy tariff backtester
        </h1>
        <p class="text-sm text-gray-500 mt-1 max-w-2xl">
          Cost your real half-hourly usage — import, export and gas — against any tariffs you
          define. CSV data is parsed in your browser and never uploaded; only an anonymised daily
          profile is sent for costing. Nothing is stored server-side.
        </p>
      </header>

      <section aria-labelledby="data-heading" class="space-y-4">
        <h2 id="data-heading" class="text-lg font-semibold text-gray-900">1 · Usage data</h2>

        <details open class="rounded-xl border border-gray-200 bg-white p-4">
          <summary class="cursor-pointer text-sm font-medium text-gray-800">
            Upload CSV files (data stays on this device)
          </summary>
          <div class="mt-4">
            <CsvImport
              @parsed="onCsvParsed"
              @gas-unit-changed="gasUnit = $event"
              @timezone-changed="sourceTz = $event"
            />
            <div class="mt-4 flex items-center gap-4">
              <button
                type="button"
                :disabled="!hasImportData || busy"
                class="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                @click="calculateFromCsv"
              >
                {{ busy ? 'Calculating…' : 'Calculate from CSV data' }}
              </button>
              <span v-if="!hasImportData" class="text-sm text-gray-400">
                Upload an import CSV first.
              </span>
            </div>
            <div class="mt-4 border-t border-gray-100 pt-4">
              <DatasetManager
                :streams="streams"
                :gas-unit="gasUnit"
                :tz="sourceTz"
                :has-data="hasImportData"
                @loaded="onDatasetLoaded"
              />
            </div>
          </div>
        </details>

        <details class="rounded-xl border border-gray-200 bg-white p-4">
          <summary class="cursor-pointer text-sm font-medium text-gray-800">
            Or connect your Octopus account
          </summary>
          <div class="mt-4">
            <OctopusConnect
              :busy="busy"
              :oauth-enabled="oauthConfig.enabled"
              :oauth-connected="oauthToken !== ''"
              @submit="calculateFromOctopus"
              @prefill="prefillTariffFromOctopus"
              @connect-octopus="startOAuthConnect"
              @disconnect="oauthToken = ''"
            />
          </div>
        </details>

        <ul v-if="dataWarnings.length" class="text-sm text-amber-700 list-disc pl-5">
          <li v-for="(w, i) in dataWarnings" :key="i">{{ w }}</li>
        </ul>
      </section>

      <section aria-labelledby="tariffs-heading" class="space-y-4">
        <div class="flex items-center justify-between">
          <h2 id="tariffs-heading" class="text-lg font-semibold text-gray-900">2 · Tariffs</h2>
          <div class="flex gap-3">
            <button
              type="button"
              class="text-sm text-indigo-600 hover:text-indigo-800 underline"
              @click="addTariff"
            >
              Add tariff
            </button>
            <button
              type="button"
              class="text-sm text-gray-500 hover:text-gray-700 underline"
              @click="resetToPresets"
            >
              Reset to presets
            </button>
          </div>
        </div>
        <div class="space-y-4">
          <TariffEditor
            v-for="(tariff, i) in tariffs"
            :key="`${tariffsVersion}-${i}`"
            :model-value="tariff"
            :index="i"
            @update:model-value="tariffs[i] = $event"
            @remove="removeTariff(i)"
          />
        </div>
      </section>

      <section aria-labelledby="results-heading" class="space-y-6">
        <h2 id="results-heading" class="text-lg font-semibold text-gray-900">3 · Results</h2>

        <p v-if="errorMessage" class="text-sm text-red-600" role="alert">{{ errorMessage }}</p>

        <template v-if="results && profile">
          <div class="rounded-xl border border-gray-200 bg-white p-4">
            <CostComparisonChart :results="results" />
          </div>
          <div class="rounded-xl border border-gray-200 bg-white p-4">
            <LoadProfileChart :profile="profile" :results="results" />
          </div>
          <p class="text-sm text-gray-500">
            Based on {{ profile.supplied_days }} days of data,
            {{ profile.import_hh.reduce((a, b) => a + b, 0).toFixed(1) }} kWh imported<template
              v-if="profile.export_hh"
            >
              , {{ profile.export_hh.reduce((a, b) => a + b, 0).toFixed(1) }} kWh exported</template
            ><template v-if="profile.gas_kwh > 0">
              , {{ profile.gas_kwh.toFixed(1) }} kWh of gas</template
            >.
          </p>
        </template>
        <p v-else class="text-sm text-gray-400">
          Provide usage data and at least one tariff, then calculate.
        </p>
      </section>

      <footer class="border-t border-gray-200 pt-6 pb-10">
        <DeleteMyData @deleted="onAllDataDeleted" />
      </footer>
    </main>
  </div>
</template>
