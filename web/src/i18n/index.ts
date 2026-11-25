import { writable, derived } from "svelte/store";
import { en, type Dict } from "./en";
import { es } from "./es";

export type Locale = "en" | "es";
export type { Dict };

export const dictionaries: Record<Locale, Dict> = { en, es };

const STORAGE_KEY = "wg.locale";

function detectLocale(): Locale {
  if (typeof window === "undefined") return "en";
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved === "en" || saved === "es") return saved;
  } catch {
    // Private mode: fall through to the browser language.
  }
  return (navigator.language || "en").toLowerCase().startsWith("es") ? "es" : "en";
}

export const locale = writable<Locale>(detectLocale());
export const dict = derived(locale, ($locale) => dictionaries[$locale]);

export function setLocale(next: Locale): void {
  locale.set(next);
  if (typeof document !== "undefined") {
    document.documentElement.lang = next;
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Persistence is best effort.
  }
}

export function format(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (_, key: string) => String(values[key] ?? `{${key}}`));
}
