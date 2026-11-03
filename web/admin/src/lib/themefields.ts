import type { ChallengeField } from "./types";

/** Form state for one field: strings for inputs, a boolean for checkboxes. */
export type FieldInput = string | boolean | { city: string; lat: string; lon: string };

function str(v: unknown): string {
  return v === undefined || v === null ? "" : String(v);
}

/** Turns stored theme_fields into form state for the given fields. */
export function toInputs(fields: ChallengeField[], stored: Record<string, unknown>): Record<string, FieldInput> {
  const out: Record<string, FieldInput> = {};
  for (const f of fields) {
    const v = stored[f.key];
    if (f.type === "boolean") out[f.key] = v === true;
    else if (f.type === "location") {
      const l = (v && typeof v === "object" ? v : {}) as Record<string, unknown>;
      out[f.key] = { city: str(l.city), lat: str(l.lat), lon: str(l.lon) };
    } else out[f.key] = str(v);
  }
  return out;
}

/**
 * Turns form state back into theme_fields. Keys the active theme doesn't
 * use are kept, since another theme may need them. Emptied fields are
 * removed. Bad numbers are sent as text, so the server explains them.
 */
export function fromInputs(
  fields: ChallengeField[], inputs: Record<string, FieldInput>, stored: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = { ...stored };
  const num = (s: string): number | string => (s.trim() !== "" && !Number.isNaN(Number(s)) ? Number(s) : s);
  for (const f of fields) {
    const v = inputs[f.key];
    delete out[f.key];
    if (f.type === "boolean") {
      if (v === true) out[f.key] = true;
    } else if (f.type === "location" && typeof v === "object") {
      if (v.city.trim() || v.lat.trim() || v.lon.trim()) {
        out[f.key] = { ...(v.city.trim() ? { city: v.city.trim() } : {}), lat: num(v.lat), lon: num(v.lon) };
      }
    } else if (typeof v === "string" && v.trim() !== "") {
      out[f.key] = f.type === "number" ? num(v) : v;
    }
  }
  return out;
}
