/**
 * IndexedDB persistence for usage datasets: tens of thousands of raw rows,
 * far too big for localStorage. Raw readings are stored (not just profiles)
 * so future features — e.g. a cumulative-cost-over-time chart needing
 * day-level resolution — can be computed client-side without re-import.
 *
 * Nothing here ever syncs anywhere: this is the device-local half of the
 * privacy story. The Octopus API key must never be written into a dataset.
 */
import { openDB, type IDBPDatabase } from 'idb'

import type { RawReading } from './types'

const DB_NAME = 'ukeb'
const DB_VERSION = 1
const STORE = 'datasets'

export interface StoredDataset {
  /** User-chosen name, e.g. "2025 full year"; the primary key. */
  name: string
  createdAt: number
  streams: {
    import?: RawReading[]
    export?: RawReading[]
    gas?: RawReading[]
  }
  gasUnit: 'kwh' | 'm3'
  /** Source timezone the CSVs were interpreted in, for provenance. */
  tz: string
}

export interface DatasetSummary {
  name: string
  createdAt: number
}

function db(): Promise<IDBPDatabase> {
  return openDB(DB_NAME, DB_VERSION, {
    upgrade(database) {
      if (!database.objectStoreNames.contains(STORE)) {
        database.createObjectStore(STORE, { keyPath: 'name' })
      }
    },
  })
}

export async function saveDataset(dataset: StoredDataset): Promise<void> {
  const conn = await db()
  try {
    await conn.put(STORE, dataset)
  } finally {
    conn.close()
  }
}

export async function listDatasets(): Promise<DatasetSummary[]> {
  const conn = await db()
  try {
    const all = (await conn.getAll(STORE)) as StoredDataset[]
    return all
      .map(({ name, createdAt }) => ({ name, createdAt }))
      .sort((a, b) => b.createdAt - a.createdAt)
  } finally {
    conn.close()
  }
}

export async function loadDataset(name: string): Promise<StoredDataset | undefined> {
  const conn = await db()
  try {
    return (await conn.get(STORE, name)) as StoredDataset | undefined
  } finally {
    conn.close()
  }
}

export async function deleteDataset(name: string): Promise<void> {
  const conn = await db()
  try {
    await conn.delete(STORE, name)
  } finally {
    conn.close()
  }
}

/** Wipes every stored dataset. Used by the "delete my data" control. */
export async function clearDatasets(): Promise<void> {
  const conn = await db()
  try {
    await conn.clear(STORE)
  } finally {
    conn.close()
  }
}
