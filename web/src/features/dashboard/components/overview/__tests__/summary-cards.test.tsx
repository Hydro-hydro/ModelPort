/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { SummaryCards } from '../summary-cards'

vi.mock('@/features/dashboard/api', () => ({
  getUserQuotaDates: vi.fn().mockResolvedValue({ success: true, data: [] }),
}))

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => ({
    status: { display_in_currency: false },
    loading: false,
    error: null,
  }),
}))

function renderSummaryCards() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <SummaryCards />
    </QueryClientProvider>
  )
}

describe('SummaryCards usage-only layout', () => {
  test('shows usage statistics without wallet balance or runway indicators', () => {
    renderSummaryCards()

    expect(screen.getByText('Usage at a glance')).toBeInTheDocument()
    expect(
      screen.getByText('Monitor usage and request volume')
    ).toBeInTheDocument()
    expect(screen.getByText('Last 24h usage')).toBeInTheDocument()
    expect(screen.getByText('Historical Usage')).toBeInTheDocument()
    expect(screen.getByText('Request Count')).toBeInTheDocument()
    expect(screen.queryByText('Credit remaining')).not.toBeInTheDocument()
    expect(screen.queryByText('Runway')).not.toBeInTheDocument()
    expect(screen.queryByText('Balance depleted')).not.toBeInTheDocument()
  })
})
