const ZERO_DECIMAL_CURRENCIES = new Set([
  "BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF",
  "UGX", "UYI", "VND", "VUV", "XAF", "XOF", "XPF",
]);

export function formatNumber(value: number | null | undefined, locale: string): string {
  if (value === null || value === undefined || Number.isNaN(value)) return "—";
  return new Intl.NumberFormat(locale).format(value);
}

export function formatAmount(minor: number | null | undefined, currency: string, locale: string): string {
  if (minor === null || minor === undefined || Number.isNaN(minor)) return "—";
  const code = (currency || "").toUpperCase();
  try {
    const major = ZERO_DECIMAL_CURRENCIES.has(code) ? minor : minor / 100;
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: code,
      maximumFractionDigits: ZERO_DECIMAL_CURRENCIES.has(code) ? 0 : 2,
    }).format(major);
  } catch {
    return `${formatNumber(minor, locale)} ${code}`.trim();
  }
}

function parseDate(iso: string | null | undefined): Date | null {
  if (!iso) return null;
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDateTime(iso: string | null | undefined, locale: string): string {
  const date = parseDate(iso);
  if (!date) return "—";
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "medium" }).format(date);
}

export function formatDate(iso: string | null | undefined, locale: string): string {
  const date = parseDate(iso);
  if (!date) return "—";
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}

export function formatTime(iso: string | null | undefined, locale: string): string {
  const date = parseDate(iso);
  if (!date) return "—";
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(date);
}

export function formatDateTimeShort(iso: string | null | undefined, locale: string): string {
  const date = parseDate(iso);
  if (!date) return "—";
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function relativeTime(iso: string | null | undefined, locale: string): string {
  const date = parseDate(iso);
  if (!date) return "—";
  const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
  const absolute = Math.abs(diffSeconds);
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

  if (absolute < 45) return formatter.format(0, "second");
  if (absolute < 90) return formatter.format(Math.round(diffSeconds / 60), "minute");
  if (absolute < 3600) return formatter.format(Math.round(diffSeconds / 60), "minute");
  if (absolute < 86400) return formatter.format(Math.round(diffSeconds / 3600), "hour");
  if (absolute < 2592000) return formatter.format(Math.round(diffSeconds / 86400), "day");
  if (absolute < 31536000) return formatter.format(Math.round(diffSeconds / 2592000), "month");
  return formatter.format(Math.round(diffSeconds / 31536000), "year");
}
