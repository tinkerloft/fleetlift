// Provide a working localStorage implementation for jsdom 28+
// jsdom 28 requires a file path for localStorage persistence; without one,
// localStorage is a stub with no methods. We replace it with a simple
// in-memory implementation for tests.
const makeLocalStorage = () => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, value: string) => { store[key] = String(value) },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
    get length() { return Object.keys(store).length },
    key: (i: number) => Object.keys(store)[i] ?? null,
  }
}

Object.defineProperty(window, 'localStorage', {
  value: makeLocalStorage(),
  writable: true,
})
