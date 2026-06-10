/**
 * Single registration point for ECharts. Modular imports keep the embedded
 * bundle small: only the chart types and components the app actually renders
 * are compiled in. Import VChart from here, never from 'vue-echarts'
 * directly, so registration always precedes first render.
 */
import { use } from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  MarkAreaComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

use([
  BarChart,
  LineChart,
  GridComponent,
  LegendComponent,
  MarkAreaComponent,
  TooltipComponent,
  CanvasRenderer,
])

export { VChart }

/** The 48 bucket labels: "00:00", "00:30", … "23:30". */
export const BUCKET_LABELS: string[] = Array.from({ length: 48 }, (_, i) => {
  const h = String(Math.floor(i / 2)).padStart(2, '0')
  return `${h}:${i % 2 === 0 ? '00' : '30'}`
})

/** Format pence as a pounds string for labels and tables. */
export function pounds(pence: number): string {
  return `£${(pence / 100).toFixed(2)}`
}
