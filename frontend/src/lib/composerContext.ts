/**
 * Composer context — the hook extensions use to read and replace the live
 * composer's draft without reaching into Composer.svelte internals.
 *
 * Composer.svelte provides the implementation via setContext; extension
 * components (e.g. the ai-writing toolbar button + review dialog) consume
 * it via getContext(COMPOSER_CONTEXT_KEY). The dialog lives inside the
 * composer window that owns the editor, so the same context serves both
 * the inline composer and the detached composer window.
 *
 * All body accessors are mode-aware: in rich-text mode they operate on the
 * TipTap editor's HTML; in plain-text mode they convert to/from the plain
 * text content, so callers always deal in HTML.
 */
export interface ComposerContext {
  /** User-composed part of the body as HTML, excluding quoted/forwarded content. */
  getUserComposedHTML(): string
  /** Quoted/forwarded part of the body as HTML ('' when there is none). */
  getQuotedHTML(): string
  /**
   * Replace ONLY the user-composed part of the body, keeping the quoted /
   * forwarded part untouched.
   */
  setUserComposedHTML(html: string): void
  /** Current subject line. */
  getSubject(): string
  /** Replace the subject line. */
  setSubject(subject: string): void
  /** True while the composer is in plain-text mode. */
  isPlainText(): boolean
}

export const COMPOSER_CONTEXT_KEY = 'composer-context'
