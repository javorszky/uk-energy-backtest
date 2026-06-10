import { describe, expect, it } from 'vitest'

import { detectPreset, parseCsv, presets } from '../csv'

const octopusCsv = ` Consumption (kwh),Start,End
0.125,2024-07-15T00:00:00+01:00,2024-07-15T00:30:00+01:00
0.5,2024-07-15T00:30:00+01:00,2024-07-15T01:00:00+01:00
`

const n3rgyCsv = `timestamp (UTC),energyConsumption (kWh)
2024-07-15 00:00,0.3
2024-07-15 00:30,0.4
`

const genericCsv = `when,usage
15/07/2024 10:00,1.5
15/07/2024 10:30,bogus
,0.2
15/07/2024 11:00,2.5
`

describe('detectPreset', () => {
  it('recognises an Octopus export including padded headers', () => {
    expect(detectPreset([' Consumption (kwh)', 'Start', 'End']).id).toBe('octopus')
  })

  it('recognises an n3rgy export', () => {
    expect(detectPreset(['timestamp (UTC)', 'energyConsumption (kWh)']).id).toBe('n3rgy')
  })

  it('falls back to generic', () => {
    expect(detectPreset(['when', 'usage']).id).toBe('generic')
  })
})

describe('parseCsv', () => {
  it('parses an Octopus export, honouring the embedded offset', async () => {
    const preset = presets.find((p) => p.id === 'octopus')!
    const mapping = preset.mapping([' Consumption (kwh)', 'Start', 'End'])!
    const { readings, warnings } = await parseCsv(octopusCsv, mapping)
    expect(warnings).toHaveLength(0)
    expect(readings).toHaveLength(2)
    // 00:00+01:00 is 23:00Z the previous day.
    expect(readings[0]).toEqual({ ts: Date.parse('2024-07-14T23:00:00Z'), value: 0.125 })
  })

  it('parses an n3rgy export as UTC', async () => {
    const preset = presets.find((p) => p.id === 'n3rgy')!
    const mapping = preset.mapping(['timestamp (UTC)', 'energyConsumption (kWh)'])!
    const { readings } = await parseCsv(n3rgyCsv, mapping, preset.forcedTz!)
    expect(readings[0]).toEqual({ ts: Date.parse('2024-07-15T00:00:00Z'), value: 0.3 })
  })

  it('parses a generic UK-format CSV in the selected zone, skipping bad rows with warnings', async () => {
    const { readings, warnings } = await parseCsv(genericCsv, {
      timestampCol: 'when',
      valueCol: 'usage',
    })
    expect(readings).toHaveLength(2)
    // 10:00 London wall time in July is 09:00Z.
    expect(readings[0]).toEqual({ ts: Date.parse('2024-07-15T09:00:00Z'), value: 1.5 })
    expect(readings[1].value).toBe(2.5)
    expect(warnings).toHaveLength(2) // bogus value + empty timestamp
  })

  it('warns when nothing parses', async () => {
    const { readings, warnings } = await parseCsv('a,b\n1,2\n', {
      timestampCol: 'a',
      valueCol: 'b',
    })
    expect(readings).toHaveLength(0)
    expect(warnings.some((w) => w.includes('No usable rows'))).toBe(true)
  })
})
