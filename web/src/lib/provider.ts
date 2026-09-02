// The backend reports the active adapter in the snapshot (`site`); the UI
// shows its readable label. Only the gemini adapter exists today; unknown
// sites fall back to their raw id.
export function providerLabel(site?: string): string {
  return site === undefined || site === 'gemini' ? 'Gemini' : site
}
