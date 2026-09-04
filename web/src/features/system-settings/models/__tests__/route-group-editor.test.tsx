/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

const { fireEvent, render, within } = await import('@testing-library/react')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { useState } = await import('react')
const { RouteGroupEditor } = await import('../route-group-editor')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
})

type EditorState = {
  GroupRatio: string
  GroupGroupRatio: string
  AutoGroups: string
}

const initialState: EditorState = {
  GroupRatio: '{"default":1,"premium":0.5}',
  GroupGroupRatio: '{"default":{"premium":0.3},"premium":{"default":0.8}}',
  AutoGroups: '["default","premium"]',
}

function Harness({ value = initialState }: { value?: EditorState }) {
  const [state, setState] = useState<EditorState>(value)
  const [visualValidity, setVisualValidity] = useState(true)

  return (
    <I18nextProvider i18n={i18n}>
      <RouteGroupEditor
        groupRatio={state.GroupRatio}
        groupGroupRatio={state.GroupGroupRatio}
        autoGroups={state.AutoGroups}
        onChange={(field, value) =>
          setState((current) => ({ ...current, [field]: value }))
        }
        onValidityChange={setVisualValidity}
      />
      <output data-testid='group-ratio'>{state.GroupRatio}</output>
      <output data-testid='group-group-ratio'>{state.GroupGroupRatio}</output>
      <output data-testid='auto-groups'>{state.AutoGroups}</output>
      <output data-testid='visual-validity'>{String(visualValidity)}</output>
    </I18nextProvider>
  )
}

function getCard(container: HTMLElement, title: string): HTMLElement {
  const titleElement = within(container).getByText(title, {
    selector: '[data-slot="card-title"]',
  })
  const card = titleElement.closest<HTMLElement>('[data-slot="card"]')
  if (!card) throw new Error(`Expected card for ${title}`)
  return card
}

function readOutput(container: HTMLElement, testId: string) {
  return JSON.parse(within(container).getByTestId(testId).textContent ?? '')
}

describe('RouteGroupEditor', () => {
  test('adds a route group from the pricing table', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')

    fireEvent.click(
      within(pricingCard).getByRole('button', { name: 'Add group' })
    )

    expect(readOutput(container, 'group-ratio')).toEqual({
      default: 1,
      premium: 0.5,
      group_1: 1,
    })
    expect(
      within(pricingCard).getAllByRole('textbox', { name: 'Group name' })[0]
    ).toHaveValue('default')
  })

  test('renames a route group on blur while updating only override targets', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')
    const groupInputs = within(pricingCard).getAllByRole('textbox', {
      name: 'Group name',
    })

    fireEvent.change(groupInputs[0], { target: { value: 'standard' } })
    fireEvent.blur(groupInputs[0])

    expect(readOutput(container, 'group-ratio')).toEqual({
      standard: 1,
      premium: 0.5,
    })
    expect(readOutput(container, 'group-group-ratio')).toEqual({
      default: { premium: 0.3 },
      premium: { standard: 0.8 },
    })
    expect(readOutput(container, 'auto-groups')).toEqual([
      'standard',
      'premium',
    ])
  })

  test('deleting a route group removes target references but preserves source maps', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')

    fireEvent.click(
      within(pricingCard).getByRole('button', { name: 'Remove default' })
    )

    expect(readOutput(container, 'group-ratio')).toEqual({ premium: 0.5 })
    expect(readOutput(container, 'group-group-ratio')).toEqual({
      default: { premium: 0.3 },
      premium: {},
    })
    expect(readOutput(container, 'auto-groups')).toEqual(['premium'])
  })

  test('deleting an uncommitted rename clears both old and current references', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')
    const groupInputs = within(pricingCard).getAllByRole('textbox', {
      name: 'Group name',
    })

    fireEvent.change(groupInputs[0], { target: { value: 'standard' } })
    fireEvent.click(
      within(pricingCard).getByRole('button', { name: 'Remove standard' })
    )

    expect(readOutput(container, 'group-ratio')).toEqual({ premium: 0.5 })
    expect(readOutput(container, 'group-group-ratio')).toEqual({
      default: { premium: 0.3 },
      premium: {},
    })
    expect(readOutput(container, 'auto-groups')).toEqual(['premium'])
  })

  test('moves automatic route groups in priority order', () => {
    const { container } = render(<Harness />)
    const autoCard = getCard(container, 'Auto assignment order')

    fireEvent.click(
      within(autoCard).getByRole('button', { name: 'Move default down' })
    )

    expect(readOutput(container, 'auto-groups')).toEqual(['premium', 'default'])
  })

  test('reports invalid visual state when a group ratio is cleared', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')
    const ratioInput = within(pricingCard).getAllByRole('spinbutton', {
      name: 'Ratio',
    })[0]

    fireEvent.change(ratioInput, { target: { value: '' } })

    expect(ratioInput).toHaveAttribute('aria-invalid', 'true')
    expect(within(container).getByTestId('visual-validity')).toHaveTextContent(
      'false'
    )
  })

  test('reports invalid visual state when a group name is cleared', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')
    const groupInput = within(pricingCard).getAllByRole('textbox', {
      name: 'Group name',
    })[0]

    fireEvent.change(groupInput, { target: { value: '' } })

    expect(within(container).getByTestId('visual-validity')).toHaveTextContent(
      'false'
    )
  })

  test('reports invalid visual state when a group uses the reserved auto name', () => {
    const { container } = render(<Harness />)
    const pricingCard = getCard(container, 'Pricing groups')
    const groupInput = within(pricingCard).getAllByRole('textbox', {
      name: 'Group name',
    })[0]

    fireEvent.change(groupInput, { target: { value: 'auto' } })

    expect(within(container).getByTestId('visual-validity')).toHaveTextContent(
      'false'
    )
    expect(readOutput(container, 'group-ratio')).toEqual({
      default: 1,
      premium: 0.5,
    })
  })

  test('reports invalid visual state for malformed automatic group JSON', () => {
    const { container } = render(
      <Harness value={{ ...initialState, AutoGroups: '["default",' }} />
    )

    expect(within(container).getByTestId('visual-validity')).toHaveTextContent(
      'false'
    )
  })

  test('keeps unknown override references visible and marks them as unknown', () => {
    const { container } = render(
      <Harness
        value={{
          ...initialState,
          GroupGroupRatio:
            '{"legacy":{"default":0.4},"default":{"legacy":0.7}}',
          AutoGroups: '["legacy","default"]',
        }}
      />
    )
    const overrideCard = getCard(container, 'Inter-group ratio overrides')

    expect(readOutput(container, 'group-group-ratio')).toEqual({
      legacy: { default: 0.4 },
      default: { legacy: 0.7 },
    })
    expect(
      within(overrideCard).getAllByLabelText('Not in pricing table')
    ).toHaveLength(2)
    expect(
      within(getCard(container, 'Auto assignment order')).getByText(
        'Not in pricing table'
      )
    ).toBeInTheDocument()
  })

  test('uses a collision-proof key for override rows with colon names', () => {
    const { container } = render(
      <Harness
        value={{
          GroupRatio: '{"a":1,"a:b":1,"b":1,"c":1}',
          GroupGroupRatio: '{"a":{"b:c":0.4},"a:b":{"c":0.5}}',
          AutoGroups: '[]',
        }}
      />
    )
    const overrideCard = getCard(container, 'Inter-group ratio overrides')
    const rows = within(overrideCard).getAllByRole('row')

    expect(rows).toHaveLength(3)
    expect(within(rows[1]).getByText('a')).toBeInTheDocument()
    expect(within(rows[1]).getByText('b:c')).toBeInTheDocument()
    expect(within(rows[2]).getByText('a:b')).toBeInTheDocument()
    expect(within(rows[2]).getByText('c')).toBeInTheDocument()
  })

  test('exposes accessible names for override group selectors', () => {
    const { container } = render(<Harness />)
    const overrideCard = getCard(container, 'Inter-group ratio overrides')

    fireEvent.click(
      within(overrideCard).getByRole('button', { name: 'Add ratio override' })
    )

    const dialog = within(document.body).getByRole('dialog')
    expect(
      within(dialog).getByRole('combobox', { name: 'Route group' })
    ).toBeInTheDocument()
    expect(
      within(dialog).getByRole('combobox', { name: 'Target group' })
    ).toBeInTheDocument()
  })
})
