import { describe, expect, it, vi } from 'vitest'

const { validateOpenAISessionTokenMock } = vi.hoisted(() => ({
  validateOpenAISessionTokenMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      refreshOpenAIToken: vi.fn(),
      validateOpenAISessionToken: validateOpenAISessionTokenMock
    }
  }
}))

import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'

describe('useOpenAIOAuth.buildCredentials', () => {
  it('should keep client_id when token response contains it', () => {
    const oauth = useOpenAIOAuth({ platform: 'sora' })
    const creds = oauth.buildCredentials({
      access_token: 'at',
      refresh_token: 'rt',
      client_id: 'app_sora_client',
      expires_at: 1700000000
    })

    expect(creds.client_id).toBe('app_sora_client')
    expect(creds.access_token).toBe('at')
    expect(creds.refresh_token).toBe('rt')
  })

  it('should keep session_token when token response contains it', () => {
    const oauth = useOpenAIOAuth({ platform: 'openai' })
    const creds = oauth.buildCredentials({
      access_token: 'at',
      session_token: 'st',
      expires_at: 1700000000
    })

    expect(creds.session_token).toBe('st')
    expect(creds.access_token).toBe('at')
  })

  it('should keep legacy behavior when client_id is missing', () => {
    const oauth = useOpenAIOAuth({ platform: 'openai' })
    const creds = oauth.buildCredentials({
      access_token: 'at',
      refresh_token: 'rt',
      expires_at: 1700000000
    })

    expect(Object.prototype.hasOwnProperty.call(creds, 'client_id')).toBe(false)
    expect(creds.access_token).toBe('at')
    expect(creds.refresh_token).toBe('rt')
  })
})

describe('useOpenAIOAuth.validateSessionToken', () => {
  it('uses the OpenAI session-token endpoint for OpenAI accounts', async () => {
    validateOpenAISessionTokenMock.mockResolvedValueOnce({ access_token: 'at' })

    const oauth = useOpenAIOAuth({ platform: 'openai' })
    await oauth.validateSessionToken('st-openai', 1)

    expect(validateOpenAISessionTokenMock).toHaveBeenCalledWith(
      'st-openai',
      1,
      '/admin/openai/st2at'
    )
  })

  it('uses the Sora session-token endpoint for Sora accounts', async () => {
    validateOpenAISessionTokenMock.mockResolvedValueOnce({ access_token: 'at' })

    const oauth = useOpenAIOAuth({ platform: 'sora' })
    await oauth.validateSessionToken('st-sora', 2)

    expect(validateOpenAISessionTokenMock).toHaveBeenCalledWith(
      'st-sora',
      2,
      '/admin/sora/st2at'
    )
  })
})
