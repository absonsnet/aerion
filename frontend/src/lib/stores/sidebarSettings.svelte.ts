// Sidebar layout settings.
//
// These two settings are persisted in localStorage, NOT in the backend
// settings table: Phase B is frontend-only, and backend settings keys need
// Go getter/setter bindings. Moving them to backend keys is a small
// follow-up (add the keys to internal/settings and App bindings); the key
// names and value shapes below already match that future layout.
//
// - sidebar_expanded_accounts: JSON array of account IDs whose folder list
//   is expanded. On launch the Sidebar clears this to an empty array, so
//   every account starts collapsed; expanding an account during a session
//   is recorded here but does not survive the next launch.
// - sidebar_hide_unified_single_account: JSON boolean, default off. Hides
//   the Unified zone when there is exactly one account.
// - unified_folder_order: JSON array of unified folder type keys
//   (inbox/starred/sent/drafts/spam/trash/outbox) in display order.
//   First run (key absent): the canonical order below. Unknown keys are
//   dropped and missing known keys are appended in canonical order.

const KEY_EXPANDED_ACCOUNTS = 'sidebar_expanded_accounts'
const KEY_HIDE_UNIFIED_SINGLE = 'sidebar_hide_unified_single_account'
const KEY_UNIFIED_FOLDER_ORDER = 'unified_folder_order'

// Canonical order of the Unified folders (Abid's list, v0.7.1).
export const UNIFIED_FOLDER_TYPES = [
  'inbox',
  'starred',
  'sent',
  'drafts',
  'spam',
  'trash',
  'outbox',
] as const
export type UnifiedFolderType = (typeof UNIFIED_FOLDER_TYPES)[number]

let expandedAccountIds = $state<string[]>([])
let hideUnifiedSingleAccount = $state(false)
let unifiedFolderOrder = $state<string[]>([...UNIFIED_FOLDER_TYPES])
// True once loadSidebarSettings has read localStorage at least once.
let loaded = false
// True when the expanded-accounts key exists in storage. Distinguishes
// "user collapsed everything" (stored empty array) from "key never
// written yet" (key absent). The Sidebar writes an empty array on its
// first run at launch, so a missing key only ever means "not launched
// with the collapse behaviour yet".
let hasStoredExpanded = false

function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch (err) {
    console.error(`Failed to persist sidebar setting ${key}:`, err)
  }
}

// Load both settings from localStorage. Safe to call repeatedly; later
// calls re-read storage (e.g. after another window changed it).
export function loadSidebarSettings(): void {
  const storedExpanded = readStored(KEY_EXPANDED_ACCOUNTS)
  hasStoredExpanded = storedExpanded !== null
  if (storedExpanded !== null) {
    try {
      const parsed: unknown = JSON.parse(storedExpanded)
      expandedAccountIds = Array.isArray(parsed)
        ? parsed.filter((id): id is string => typeof id === 'string')
        : []
    } catch {
      expandedAccountIds = []
    }
  }

  const storedHide = readStored(KEY_HIDE_UNIFIED_SINGLE)
  hideUnifiedSingleAccount = storedHide === 'true'

  const storedOrder = readStored(KEY_UNIFIED_FOLDER_ORDER)
  if (storedOrder !== null) {
    try {
      const parsed: unknown = JSON.parse(storedOrder)
      if (Array.isArray(parsed)) {
        const known = new Set<string>(UNIFIED_FOLDER_TYPES)
        const ordered = parsed.filter(
          (id): id is string => typeof id === 'string' && known.has(id)
        )
        // Drop duplicates, then append any known type the stored order
        // is missing (e.g. a type added by an upgrade) in canonical order.
        const seen = new Set<string>()
        const deduped = ordered.filter((id) => {
          if (seen.has(id)) return false
          seen.add(id)
          return true
        })
        for (const type of UNIFIED_FOLDER_TYPES) {
          if (!seen.has(type)) deduped.push(type)
        }
        unifiedFolderOrder = deduped
      }
    } catch {
      unifiedFolderOrder = [...UNIFIED_FOLDER_TYPES]
    }
  }
  loaded = true
}

// True when the expanded-accounts key has never been stored — i.e. the
// app has not launched with the collapse-on-launch behaviour yet.
export function hasStoredExpandedAccounts(): boolean {
  if (!loaded) loadSidebarSettings()
  return hasStoredExpanded
}

export function getExpandedAccountIds(): string[] {
  if (!loaded) loadSidebarSettings()
  return expandedAccountIds
}

export function isSidebarAccountExpanded(accountId: string): boolean {
  if (!loaded) loadSidebarSettings()
  return expandedAccountIds.includes(accountId)
}

export function setSidebarAccountExpanded(accountId: string, expanded: boolean): void {
  if (!loaded) loadSidebarSettings()
  const next = expanded
    ? [...new Set([...expandedAccountIds, accountId])]
    : expandedAccountIds.filter((id) => id !== accountId)
  expandedAccountIds = next
  hasStoredExpanded = true
  writeStored(KEY_EXPANDED_ACCOUNTS, JSON.stringify(next))
}

// Replace the whole expanded set (first-run default, pruning stale IDs).
export function setExpandedAccountIds(ids: string[]): void {
  if (!loaded) loadSidebarSettings()
  expandedAccountIds = [...ids]
  hasStoredExpanded = true
  writeStored(KEY_EXPANDED_ACCOUNTS, JSON.stringify(expandedAccountIds))
}

export function getHideUnifiedSingleAccount(): boolean {
  if (!loaded) loadSidebarSettings()
  return hideUnifiedSingleAccount
}

export function setHideUnifiedSingleAccount(value: boolean): void {
  if (!loaded) loadSidebarSettings()
  hideUnifiedSingleAccount = value
  writeStored(KEY_HIDE_UNIFIED_SINGLE, JSON.stringify(value))
}

// Unified folder display order (drag-to-reorder in the sidebar).
export function getUnifiedFolderOrder(): string[] {
  if (!loaded) loadSidebarSettings()
  return [...unifiedFolderOrder]
}

export function setUnifiedFolderOrder(order: string[]): void {
  if (!loaded) loadSidebarSettings()
  const known = new Set<string>(UNIFIED_FOLDER_TYPES)
  const seen = new Set<string>()
  const cleaned = order.filter((id) => {
    if (!known.has(id) || seen.has(id)) return false
    seen.add(id)
    return true
  })
  for (const type of UNIFIED_FOLDER_TYPES) {
    if (!seen.has(type)) cleaned.push(type)
  }
  unifiedFolderOrder = cleaned
  writeStored(KEY_UNIFIED_FOLDER_ORDER, JSON.stringify(cleaned))
}
