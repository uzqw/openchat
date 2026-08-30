import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// vitest's jsdom environment provides fetch/Response via undici globals,
// so the api client and the fetch stub below work as in a browser.

afterEach(() => {
  cleanup()
})
