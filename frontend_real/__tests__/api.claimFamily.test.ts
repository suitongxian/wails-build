import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { api } from '../services/api'

describe('api.analyzePreview', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          cache_miss_count: 12,
          last_run_at: '2026-05-26T10:00:00Z',
          last_run_duration_sec: 252,
        },
      }),
    })))
  })

  afterEach(() => { vi.unstubAllGlobals() })

  it('returns AnalyzePreview shape', async () => {
    const r = await api.analyzePreview()
    expect(r.cache_miss_count).toBe(12)
    expect(r.last_run_duration_sec).toBe(252)
    expect(r.last_run_at).toBe('2026-05-26T10:00:00Z')
  })

  it('calls POST /similarity/analyze/preview', async () => {
    await api.analyzePreview()
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, opts] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/similarity/analyze/preview')
    expect((opts as RequestInit)?.method).toBe('POST')
  })

  it('throws on non-success response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: false, error: 'something broke' }),
    })))
    await expect(api.analyzePreview()).rejects.toThrow('something broke')
  })
})

describe('api.batchFamilyMembers', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('returns map keyed by content_sign', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          'CS_1': [{
            data_resources_id: 1, family_id: 10, family_relation: 'primary',
            content_sign: 'CS_1', resources_name: 'a.pdf', source_count: 1,
            claim_status: 0, family_score: 1.0, claimant_name: null, claim_time: null,
            data_distribution_id: null, path: null, ip: null,
          }],
        },
      }),
    })))

    const r = await api.batchFamilyMembers(['CS_1'])
    expect(r['CS_1']).toHaveLength(1)
    expect(r['CS_1'][0].family_relation).toBe('primary')
  })

  it('calls POST /family/batch-members with body', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: true, data: {} }),
    })))
    await api.batchFamilyMembers(['CS_A', 'CS_B'])
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, opts] = fetchMock.mock.calls[0]
    expect(String(url)).toContain('/family/batch-members')
    expect((opts as RequestInit)?.method).toBe('POST')
    const body = JSON.parse((opts as RequestInit)?.body as string)
    expect(body.content_signs).toEqual(['CS_A', 'CS_B'])
  })

  it('returns empty object for empty input gracefully', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: true, data: {} }),
    })))
    const r = await api.batchFamilyMembers([])
    expect(r).toEqual({})
  })

  it('throws on non-success response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ success: false, error: 'batch failed' }),
    })))
    await expect(api.batchFamilyMembers(['X'])).rejects.toThrow('batch failed')
  })
})
