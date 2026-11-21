// One-shot hand-off from a Vue route (task result, material card) to the
// video editor island. sessionStorage survives the router navigation but
// not a new tab; the editor removes the entry as soon as it reads it so a
// refresh does not import the same clip twice.
const KEY = "studio.initialMedia";

export function stashStudioMedia(entries) {
  const media = (entries || []).filter((entry) => entry && entry.url && entry.name);
  if (media.length === 0) return false;
  sessionStorage.setItem(KEY, JSON.stringify(media));
  return true;
}

export function takeStudioMedia() {
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return [];
  sessionStorage.removeItem(KEY);
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}