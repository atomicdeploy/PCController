// effectiveProductTitle keeps package metadata as the build-time fallback while
// allowing the live host configuration to rename every browser surface.
export function effectiveProductTitle(configured: string | null | undefined, fallback: string): string {
  return configured?.trim() || fallback.trim()
}

export function productMark(title: string, fallback: string): string {
  const words = title.trim().split(/\s+/).filter(Boolean)
  if (words.length > 1) return words.slice(0, 2).map((word) => word[0]).join('').toUpperCase()
  return Array.from(words[0] || fallback).slice(0, 2).join('').toUpperCase()
}
