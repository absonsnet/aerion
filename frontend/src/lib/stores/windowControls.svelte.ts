// Runes-based window-controls store.
// Holds the layout detected by the host at startup (side, order, style)
// and computes the effective layout by applying the Settings override
// (Auto / Left / Right) from the settings store.

// @ts-ignore - wailsjs path
import { GetWindowControlsLayout } from '../../../wailsjs/go/app/App'
import { getWindowControlsSide } from './settings.svelte'

export type WindowControlsSide = 'left' | 'right'
export type WindowControlsStyle = 'traffic' | 'glyphs'
export type WindowControlButton = 'close' | 'minimize' | 'maximize'

export interface WindowControlsLayout {
  side: WindowControlsSide
  order: WindowControlButton[]
  style: WindowControlsStyle
}

// Canonical orders, matching the host detector: left is the macOS/Ubuntu
// convention (close first), right is the Windows/GNOME/KDE convention
// (close last).
const LEFT_ORDER: WindowControlButton[] = ['close', 'minimize', 'maximize']
const RIGHT_ORDER: WindowControlButton[] = ['minimize', 'maximize', 'close']

function defaultOrder(side: WindowControlsSide): WindowControlButton[] {
  return side === 'left' ? [...LEFT_ORDER] : [...RIGHT_ORDER]
}

function isButton(value: string): value is WindowControlButton {
  return value === 'close' || value === 'minimize' || value === 'maximize'
}

function normalizeLayout(raw: unknown): WindowControlsLayout | null {
  if (!raw || typeof raw !== 'object') return null
  const layout = raw as { side?: unknown; order?: unknown; style?: unknown }
  if (layout.side !== 'left' && layout.side !== 'right') return null
  const side = layout.side as WindowControlsSide
  const order = Array.isArray(layout.order)
    ? (layout.order.filter((b): b is WindowControlButton => typeof b === 'string' && isButton(b)))
    : []
  return {
    side,
    order: order.length === 3 ? order : defaultOrder(side),
    style: layout.style === 'glyphs' ? 'glyphs' : 'traffic',
  }
}

// Fallback used until the host layout loads (or if it fails): buttons on
// the right, traffic-light style — the layout the app has always drawn.
const FALLBACK_LAYOUT: WindowControlsLayout = {
  side: 'right',
  order: [...RIGHT_ORDER],
  style: 'traffic',
}

// Module-level reactive state
let detectedLayout = $state<WindowControlsLayout | null>(null)
let loaded = false

export function getDetectedWindowControls(): WindowControlsLayout | null {
  return detectedLayout
}

// Load the detected layout from the host once (call on app startup).
export async function loadWindowControlsLayout(): Promise<void> {
  if (loaded) return
  loaded = true
  try {
    const layout = await GetWindowControlsLayout()
    detectedLayout = normalizeLayout(layout) ?? FALLBACK_LAYOUT
  } catch (err) {
    console.error('Failed to load window controls layout:', err)
    detectedLayout = FALLBACK_LAYOUT
  }
}

// Effective layout: the Settings override (Auto / Left / Right) decides
// the side; Auto uses the detected layout as-is. Forcing a side keeps the
// detected order when it already matches that side, and otherwise uses
// the canonical order for the forced side. The style always comes from
// detection (glyph buttons on Windows, traffic lights elsewhere) — the
// override only moves the buttons, it never restyles them.
export function getEffectiveWindowControls(): WindowControlsLayout {
  const detected = detectedLayout ?? FALLBACK_LAYOUT
  const override = getWindowControlsSide()
  if (override === 'left' || override === 'right') {
    if (override === detected.side) return detected
    return { side: override, order: defaultOrder(override), style: detected.style }
  }
  return detected
}
