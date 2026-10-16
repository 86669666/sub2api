/** Shared chart colors. Hex values match --accent / primary-600 in style.css. */

export const themeAccent = '#3b6fd6'

export const donutColors = [
  '#3b6fd6',
  '#2f9e95',
  '#8ea0b5',
  '#6d8299',
  '#c5d0dc',
  '#1e4d7b',
  '#7dccc4',
  '#4b5568'
]

export function cssVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}
