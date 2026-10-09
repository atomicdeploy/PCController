const clampUsage = (value) => {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? Math.min(100, Math.max(0, numeric)) : 0;
};

export function usageColor(value) {
  const bounded = clampUsage(value);
  if (bounded > 80) return "dc3545";
  if (bounded > 50) return "fd7e14";
  return "28a745";
}

export function usageProgress(value, label) {
  const bounded = clampUsage(value);
  const rounded = Math.floor(bounded);
  const color = usageColor(bounded);
  const safeLabel = String(label ?? "Usage").replaceAll("|", "\\|");
  return `![${safeLabel} ${rounded}%](https://geps.dev/progress/${rounded}?dangerColor=${color}&warningColor=${color}&successColor=${color})`;
}
