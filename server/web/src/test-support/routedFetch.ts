import { vi } from 'vitest'

export function jsonResponse(status: number, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
    },
  })
}

export type RouteHandler = () => Response | Promise<Response>
export type Routes = Record<string, RouteHandler>
export function stubRoutedFetch(routes: Routes) {
  const fetchMock = vi.fn<typeof fetch>().mockImplementation((input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = (init?.method ?? 'GET').toUpperCase()
    const key = `${method} ${url}`
    const handler = routes[key]
    if (handler === undefined) {
      return Promise.reject(new Error(`unstubbed request: ${key}`))
    }
    return Promise.resolve(handler())
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}
