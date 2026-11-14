import './app.css'
import { initI18n } from './lib/i18n'
import ViewerApp from './ViewerApp.svelte'
import { mount } from 'svelte'

// This entry runs in a detached message-viewer popup window. Flag the
// window before mounting so viewerBindings.ts dispatches backend calls to
// the ViewerApp bindings instead of the main window's App bindings.
;(window as unknown as { __AERION_VIEWER_WINDOW__?: boolean }).__AERION_VIEWER_WINDOW__ = true

// Initialize i18n and wait for locale to load before mounting
// ($_ throws if called before the locale is ready)
async function bootstrap() {
  await initI18n()
  mount(ViewerApp, { target: document.getElementById('app')! })
}

bootstrap()
