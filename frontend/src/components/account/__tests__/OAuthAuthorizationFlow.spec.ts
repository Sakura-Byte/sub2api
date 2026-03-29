import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: ref(false),
    copyToClipboard: vi.fn()
  })
}))

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

describe('OAuthAuthorizationFlow', () => {
  it('shows the ChatGPT session fetch URL when OpenAI ST input is selected', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showSessionTokenOption: true
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    await wrapper.get('input[value="session_token"]').setValue(true)

    expect(wrapper.text()).toContain('https://chatgpt.com/api/auth/session')
    expect(wrapper.text()).not.toContain('https://sora.chatgpt.com/api/auth/session')
  })
})
