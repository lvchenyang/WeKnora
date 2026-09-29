<template>
  <main class="wecom-complete">
    <section aria-live="polite">
      <h1>{{ t('wecom.login') }}</h1>
      <t-loading v-if="pending" :text="t('wecom.completing')" />
      <template v-else>
        <p role="alert">{{ t('wecom.loginFailed') }}</p>
        <t-button theme="primary" @click="router.replace('/login')">{{ t('wecom.backToLogin') }}</t-button>
      </template>
    </section>
  </main>
</template>
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { exchangeWeComLogin } from '@/api/auth/wecom'
import { userInfoFromApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const pending = ref(true)
onMounted(async () => {
  let installed = false
  try {
    const flowId = route.query.flow_id
    if (route.query.error || typeof flowId !== 'string' || !/^[a-f0-9-]{36}$/i.test(flowId)) throw new Error('invalid_flow')
    const result = await exchangeWeComLogin(flowId)
    if (!result.success || !result.user || !result.token || !result.refresh_token) throw new Error('invalid_session')
    auth.logout()
    installed = true
    const tenant = result.active_tenant
    auth.setUser(userInfoFromApi(result.user, result.user.tenant_id))
    auth.setToken(result.token)
    auth.setRefreshToken(result.refresh_token)
    auth.setMemberships(result.memberships ?? [])
    auth.setSelectedTenant(tenant && tenant.id !== result.user.tenant_id ? tenant.id : null, tenant?.name ?? null)
    if (!await auth.refreshFromAuthMe()) throw new Error('session_unavailable')
    await router.replace(auth.hasValidTenant ? '/platform/knowledge-bases' : '/onboarding/workspace')
  } catch {
    if (installed) auth.logout()
    pending.value = false
    // Remove the spent flow identifier; refreshing this page must not retry it.
    await router.replace({ path: '/login/wecom/complete' })
  }
})
</script>
<style scoped>
.wecom-complete { min-height: 100vh; display: grid; place-items: center; background: var(--td-bg-color-page); padding: 24px; }
section { max-width: 480px; padding: 32px; background: var(--td-bg-color-container); border-radius: var(--app-radius-xl); }
h1 { font-size: var(--app-text-3xl); margin: 0 0 20px; }
p { line-height: 1.7; margin-bottom: 24px; color: var(--td-text-color-secondary); }
</style>
