/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/** Chart-ready latency point derived from the real performance endpoint. */
export type LatencyTimePoint = {
  timestamp: string
  group: string
  ttft_ms: number
}

/** Chart-ready uptime point derived from the real performance endpoint. */
export type UptimeDayPoint = {
  date: string
  uptime_pct: number
  incidents: number
  outage_minutes: number
}

/** Aggregate sampled uptime points without inventing missing measurements. */
export function aggregateUptime(points: UptimeDayPoint[]): {
  uptime_pct: number
  incidents: number
  outage_minutes: number
} {
  if (points.length === 0) {
    return { uptime_pct: 0, incidents: 0, outage_minutes: 0 }
  }

  const incidents = points.reduce((sum, point) => sum + point.incidents, 0)
  const outageMinutes = points.reduce(
    (sum, point) => sum + point.outage_minutes,
    0
  )
  const uptimePct =
    points.reduce((sum, point) => sum + point.uptime_pct, 0) / points.length

  return {
    incidents,
    outage_minutes: outageMinutes,
    uptime_pct: Math.round(uptimePct * 1000) / 1000,
  }
}
