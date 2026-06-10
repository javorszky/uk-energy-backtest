// jsdom ships no IndexedDB; fake-indexeddb provides a spec-compliant
// in-memory implementation for these tests.
import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it } from 'vitest'

import {
  clearDatasets,
  deleteDataset,
  listDatasets,
  loadDataset,
  saveDataset,
  type StoredDataset,
} from '../datasetStore'

const dataset = (name: string, createdAt: number): StoredDataset => ({
  name,
  createdAt,
  streams: {
    import: [{ ts: Date.parse('2024-06-01T10:00:00Z'), value: 1.5 }],
  },
  gasUnit: 'kwh',
  tz: 'Europe/London',
})

beforeEach(async () => {
  await clearDatasets()
})

describe('datasetStore', () => {
  it('round-trips a dataset', async () => {
    await saveDataset(dataset('2024 full year', 1000))
    const loaded = await loadDataset('2024 full year')
    expect(loaded?.streams.import).toHaveLength(1)
    expect(loaded?.gasUnit).toBe('kwh')
  })

  it('lists summaries newest first without the heavy streams', async () => {
    await saveDataset(dataset('older', 1000))
    await saveDataset(dataset('newer', 2000))
    const list = await listDatasets()
    expect(list).toEqual([
      { name: 'newer', createdAt: 2000 },
      { name: 'older', createdAt: 1000 },
    ])
  })

  it('overwrites a dataset with the same name', async () => {
    await saveDataset(dataset('x', 1000))
    await saveDataset({ ...dataset('x', 2000), gasUnit: 'm3' })
    const loaded = await loadDataset('x')
    expect(loaded?.gasUnit).toBe('m3')
    expect(await listDatasets()).toHaveLength(1)
  })

  it('deletes a single dataset', async () => {
    await saveDataset(dataset('keep', 1))
    await saveDataset(dataset('drop', 2))
    await deleteDataset('drop')
    expect((await listDatasets()).map((d) => d.name)).toEqual(['keep'])
  })

  it('clearDatasets wipes everything', async () => {
    await saveDataset(dataset('a', 1))
    await saveDataset(dataset('b', 2))
    await clearDatasets()
    expect(await listDatasets()).toHaveLength(0)
  })

  it('loadDataset returns undefined for unknown names', async () => {
    expect(await loadDataset('nope')).toBeUndefined()
  })
})
