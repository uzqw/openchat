// Provider site display names. The backend reports the active adapter in
// the snapshot (`site`); the UI shows its readable label instead of
// hardcoding "Gemini" texts that would be wrong under another provider.
export function providerLabel(site?: string): string {
  return site === 'grok' ? 'Grok' : 'Gemini'
}
