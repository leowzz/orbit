import { stripVTControlCharacters } from "node:util";

// Preserve only SGR styling. Cursor movement, screen clearing, OSC commands,
// and other controls from child processes must not alter the dashboard.
export function sanitizeLog(value, color) {
  const clean = text => stripVTControlCharacters(text).replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, "");
  if (!color) return clean(value);
  return value.split(/(\x1b\[[0-9;:]*m)/g).map(part =>
    /^\x1b\[[0-9;:]*m$/.test(part) ? part : clean(part),
  ).join("");
}
