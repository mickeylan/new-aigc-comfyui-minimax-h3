import { api } from './index'

// Kept as a focused export for catalog consumers while sharing the app HTTP client.
export function fetchModelCatalog() {
  return api.modelCatalog()
}
