import { beforeEach, describe, expect, it } from 'vitest'

import {
  clearTariffs,
  loadTariffs,
  migrate,
  presetTariffs,
  saveTariffs,
  starterPresets,
  TARIFFS_STORAGE_KEY,
} from '../tariffStore'
import type { Tariff } from '../types'

const sample: Tariff = {
  name: 'test',
  electricity: {
    standing_charge: 50,
    import_default: 25,
    import_bands: [{ from: '02:00', to: '05:00', rate: 10 }],
    export_default: 15,
    export_bands: [],
  },
}

beforeEach(() => {
  localStorage.clear()
})

describe('tariffStore', () => {
  it('round-trips tariffs through localStorage', () => {
    saveTariffs([sample])
    expect(loadTariffs()).toEqual([sample])
  })

  it('returns presets when nothing is stored', () => {
    expect(loadTariffs()).toEqual(starterPresets)
  })

  it('falls back to presets on corrupt JSON', () => {
    localStorage.setItem(TARIFFS_STORAGE_KEY, '{nope')
    expect(loadTariffs()).toEqual(starterPresets)
  })

  it('falls back to presets on an unknown schema version', () => {
    localStorage.setItem(TARIFFS_STORAGE_KEY, JSON.stringify({ version: 99, tariffs: [sample] }))
    expect(loadTariffs()).toEqual(starterPresets)
  })

  it('falls back to presets when stored tariffs fail validation', () => {
    localStorage.setItem(
      TARIFFS_STORAGE_KEY,
      JSON.stringify({ version: 1, tariffs: [{ name: 42 }] }),
    )
    expect(loadTariffs()).toEqual(starterPresets)
  })

  it('clearTariffs removes the key', () => {
    saveTariffs([sample])
    clearTariffs()
    expect(localStorage.getItem(TARIFFS_STORAGE_KEY)).toBeNull()
  })

  it('presetTariffs returns an independent copy', () => {
    const a = presetTariffs()
    a[0].name = 'mutated'
    expect(presetTariffs()[0].name).not.toBe('mutated')
    expect(starterPresets[0].name).not.toBe('mutated')
  })

  it('migrate accepts a valid current-version payload', () => {
    expect(migrate({ version: 1, tariffs: [sample] })).toEqual([sample])
  })

  it('migrate rejects junk', () => {
    expect(migrate(null)).toBeNull()
    expect(migrate('hi')).toBeNull()
    expect(migrate({ version: 1, tariffs: 'no' })).toBeNull()
  })

  it('every starter preset passes its own validation through a save/load cycle', () => {
    saveTariffs(starterPresets)
    expect(loadTariffs()).toEqual(starterPresets)
  })
})
