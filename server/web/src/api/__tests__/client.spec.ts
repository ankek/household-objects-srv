import { afterEach, describe, expect, it, vi } from 'vitest'
import { API_BASE_URL, ApiError, apiFetch } from '@/api'

function stubFetch(response: Response) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('apiFetch', () => {
  it('prefixes the versioned base path and keeps the request same-origin', async () => {
    const fetchMock = stubFetch(jsonResponse({ ok: true }))

    await apiFetch('/status')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe(`${API_BASE_URL}/status`)
    expect(url).toBe('/api/v1/status')
    expect(init?.credentials).toBe('same-origin')
    expect(init?.headers).toMatchObject({ Accept: 'application/json' })
  })

  it('lets the caller add headers without dropping the Accept default', async () => {
    const fetchMock = stubFetch(jsonResponse({}))

    await apiFetch('/things', { method: 'POST', headers: { 'X-Trace': 'abc' } })

    const [, init] = fetchMock.mock.calls[0]!
    expect(init?.method).toBe('POST')
    expect(init?.headers).toMatchObject({ Accept: 'application/json', 'X-Trace': 'abc' })
  })

  it('returns the decoded JSON body', async () => {
    stubFetch(jsonResponse({ version: '1.2.3', items: 2 }))

    await expect(apiFetch<{ version: string; items: number }>('/status')).resolves.toEqual({
      version: '1.2.3',
      items: 2,
    })
  })

  it('resolves to undefined on 204 without attempting to decode a body', async () => {
    const response = new Response(null, { status: 204 })
    const json = vi.spyOn(response, 'json')
    stubFetch(response)

    await expect(apiFetch('/things/1')).resolves.toBeUndefined()
    expect(json).not.toHaveBeenCalled()
  })

  it('throws ApiError carrying the status and the problem body', async () => {
    stubFetch(jsonResponse({ title: 'Not found', detail: 'no such item', status: 404 }, 404))

    const error = await apiFetch('/things/9').catch((caught: unknown) => caught)

    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.name).toBe('ApiError')
    expect(apiError.status).toBe(404)
    expect(apiError.message).toBe('Not found')
    expect(apiError.problem).toMatchObject({ detail: 'no such item' })
  })

  it('falls back to a status message when the error body is not JSON', async () => {
    stubFetch(new Response('<html>502</html>', { status: 502 }))

    const error = (await apiFetch('/status').catch((caught: unknown) => caught)) as ApiError

    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(502)
    expect(error.problem).toBeNull()
    expect(error.message).toBe('API request failed with status 502')
  })
})
