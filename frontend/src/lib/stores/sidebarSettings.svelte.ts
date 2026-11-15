// Sidebar layout settings.
//
// These two settings are persisted in localStorage, NOT in the backend
// settings table: Phase B is frontend-only, and backend settings keys need
// Go getter/setter bindings. Moving them to backend keys is a small
// follow-up (add the keys to internal/settings and App bindings); the key
// names and value shapes below already match that future layout.
//
// - sidebar_expanded_accounts: JSON array of account IDs whose folder list
//   is expanded. First run (key absent): only the first account is expanded.
// - sidebar_hide_unified_single_account: JSON boolean, default off. Hides
//   the Unified zone when there is exactly one account.

const KEY_EXPANDED_ACCOUNTS = 'sidebar_expanded_accounts'
const KEY_HIDE_UNIFIED_SINGLE = 'sidebar_hide_unified_single_account'

let expandedAccountIds = $state<string[]>([])
let hideUnifiedSingleAccount = $state(false)
// True once loadSidebarSettings has read localStorage at least once.
let loaded = false
// True when the expanded-accounts key exists in storage. Distinguishes
// "user collapsed everything" (stored empty array) from "first run"
// (key absent → default to first account expanded).
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
  loaded = true
}

// True when the expanded-accounts key has never been stored — i.e. first
// run after the upgrade that introduced the new sidebar.
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
