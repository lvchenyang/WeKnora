<template>
  <section v-if="enabled" class="wecom-bindings" :aria-label="t('wecom.title')">
    <h3>{{ t('wecom.title') }}</h3>
    <p class="description">{{ t('wecom.description') }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <form class="binding-form" @submit.prevent="preview">
      <label>{{ t('wecom.email') }}<t-input v-model="email" type="text" :disabled="busy" :placeholder="t('wecom.emailHint')" /></label>
      <label>{{ t('wecom.subject') }}<t-input v-model="subject" :disabled="busy" :placeholder="t('wecom.subjectHint')" /></label>
      <t-button type="submit" theme="default" :loading="busy" :disabled="!email.trim() || !subject.trim()">{{ t('wecom.verify') }}</t-button>
    </form>
    <div v-if="verified" class="preview" aria-live="polite">
      <h4>{{ t('wecom.confirmTitle') }}</h4>
      <p>{{ verified.username }} · {{ verified.email }}</p>
      <p>{{ verified.display_name }} · {{ verified.subject }}</p>
      <p>{{ t('wecom.corporation') }}: {{ verified.corp_id }}</p>
      <p>{{ t('wecom.confirmHint') }}</p>
      <t-button theme="primary" :loading="busy" @click="confirmBinding">{{ t('wecom.confirm') }}</t-button>
    </div>
    <div class="list-header">
      <h4>{{ t('wecom.boundAccounts') }}</h4>
      <t-button variant="text" :disabled="busy" @click="load">{{ t('wecom.refresh') }}</t-button>
    </div>
    <t-loading v-if="loading" />
    <p v-else-if="bindings.length === 0">{{ t('wecom.empty') }}</p>
    <ul v-else class="bindings">
      <li v-for="binding in bindings" :key="binding.id">
        <div class="account">
          <strong>{{ binding.display_name }} · {{ binding.subject }}</strong>
          <span>{{ users[binding.user_id]?.email || binding.user_id }}</span>
          <span>{{ t(`wecom.${binding.status}`) }}</span>
        </div>
        <div class="actions">
          <button type="button" v-if="binding.status !== 'revoked'" class="binding-action" :disabled="busy"
            @click="requestStatusChange(binding, binding.status === 'active' ? 'suspended' : 'active')">
            {{ t(binding.status === 'active' ? 'wecom.suspend' : 'wecom.restore') }}
          </button>
          <t-button v-if="binding.status !== 'revoked'" variant="text" theme="danger" :disabled="busy"
            @click="requestStatusChange(binding, 'revoked')">{{ t('wecom.revoke') }}</t-button>
          <t-button variant="text" :disabled="busy" @click="showEvents(binding)">{{ t('wecom.history') }}</t-button>
        </div>
        <div v-if="pendingChange?.binding.id === binding.id" class="status-confirm" role="alert">
          <p>{{ t(pendingChange.status === 'revoked' ? 'wecom.revokeConfirm' : 'wecom.statusConfirm') }}</p>
          <t-button theme="primary" :loading="busy" @click="changeStatus(pendingChange.binding, pendingChange.status)">{{ t('common.confirm') }}</t-button>
          <t-button variant="text" :disabled="busy" @click="pendingChange = null">{{ t('common.cancel') }}</t-button>
        </div>
      </li>
    </ul>
    <div v-if="offset > 0 || bindings.length === 100" class="pagination">
      <t-button :disabled="offset === 0 || busy" @click="page(-100)">{{ t('wecom.previous') }}</t-button>
      <t-button :disabled="bindings.length < 100 || busy" @click="page(100)">{{ t('wecom.next') }}</t-button>
    </div>
    <t-dialog v-model:visible="historyVisible" :header="t('wecom.history')" :footer="false">
      <p v-if="events.length === 0">{{ t('wecom.emptyHistory') }}</p>
      <ul v-else class="history">
        <li v-for="event in events" :key="event.id">
          {{ new Date(event.created_at).toLocaleString() }} · {{ t(`wecom.${event.action === 'bind' ? 'bound' : event.action}`) }}
          <small>{{ t('wecom.actor') }}: {{ event.actor_id }} · {{ t('wecom.version') }} {{ event.version }}</small>
        </li>
      </ul>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { getWeComConfig, listWeComBindings, previewWeComBinding, bindWeComIdentity, setWeComBindingStatus, getWeComBindingEvents, type WeComBinding, type WeComPreview, type WeComEvent } from '@/api/auth/wecom'
const { t } = useI18n()
const enabled = ref(false), loading = ref(false), busy = ref(false)
const email = ref(''), subject = ref(''), error = ref(''), offset = ref(0)
const verified = ref<WeComPreview | null>(null)
const pendingChange = ref<{ binding: WeComBinding; status: WeComBinding['status'] } | null>(null)
const bindings = ref<WeComBinding[]>([])
const users = ref<Record<string, { email: string; username: string }>>({})
const events = ref<WeComEvent[]>([]), historyVisible = ref(false)
watch([email, subject], () => { verified.value = null })
async function load() {
  loading.value = true
  try {
    const result = await listWeComBindings(offset.value)
    pendingChange.value = null; bindings.value = result.bindings ?? []; users.value = result.users ?? {}; error.value = ''
  } catch { error.value = t('wecom.loadFailed') }
  finally { loading.value = false }
}
async function preview() {
  busy.value = true; verified.value = null; error.value = ''
  try { verified.value = await previewWeComBinding(email.value.trim(), subject.value.trim()) }
  catch { error.value = t('wecom.previewFailed') }
  finally { busy.value = false }
}
async function confirmBinding() {
  if (!verified.value) return
  busy.value = true; error.value = ''
  try {
    await bindWeComIdentity(verified.value.preview_token)
    verified.value = null; email.value = ''; subject.value = ''; offset.value = 0
    MessagePlugin.success(t('wecom.saved')); await load()
  } catch { verified.value = null; error.value = t('wecom.conflict') }
  finally { busy.value = false }
}
function requestStatusChange(binding: WeComBinding, status: WeComBinding['status']) {
  pendingChange.value = { binding, status }
}
async function changeStatus(binding: WeComBinding, status: WeComBinding['status']) {
  busy.value = true; error.value = ''
  try { await setWeComBindingStatus(binding, status); MessagePlugin.success(t('wecom.saved')); await load() }
  catch { error.value = t('wecom.conflict') }
  finally { busy.value = false }
}
async function showEvents(binding: WeComBinding) {
  busy.value = true
  try { events.value = (await getWeComBindingEvents(binding.id)).events ?? []; historyVisible.value = true }
  catch { error.value = t('wecom.loadFailed') }
  finally { busy.value = false }
}
async function page(delta: number) { offset.value = Math.max(0, offset.value + delta); await load() }
onMounted(async () => {
  try { enabled.value = (await getWeComConfig()).enabled; if (enabled.value) await load() }
  catch { enabled.value = false }
})
</script>
<style scoped>
.wecom-bindings { border: 1px solid var(--td-component-border); border-radius: var(--app-radius-md); padding: 20px; margin: 20px 0; }
h3, h4 { margin: 0 0 12px; }
.description, .account span { color: var(--td-text-color-secondary); font-size: var(--app-text-md); line-height: 1.7; }
.binding-form { display: flex; align-items: end; gap: 12px; flex-wrap: wrap; margin: 20px 0; }
.binding-form label { flex: 1; min-width: 180px; font-size: var(--app-text-md); line-height: 2; }
.preview { padding: 16px; border-radius: var(--app-radius-sm); background: var(--td-bg-color-secondarycontainer); margin-bottom: 20px; overflow-wrap: anywhere; }
.preview p { margin: 8px 0; }
.list-header { display: flex; justify-content: space-between; align-items: center; }
.bindings, .history { list-style: none; padding: 0; margin: 0; }
.bindings li { display: flex; flex-wrap: wrap; gap: 12px; justify-content: space-between; padding: 14px 0; border-top: 1px solid var(--td-component-border); }
.account { display: flex; flex-direction: column; overflow-wrap: anywhere; }
.actions { display: flex; align-items: center; flex-wrap: wrap; }
.binding-action { border: 0; background: transparent; padding: 8px 12px; cursor: pointer; color: var(--td-text-color-primary); font: inherit; }
.error { color: var(--td-error-color); }
.status-confirm { flex-basis: 100%; background: var(--td-bg-color-secondarycontainer); padding: 12px; border-radius: var(--app-radius-sm); }
.pagination { display: flex; justify-content: end; gap: 12px; margin-top: 12px; }
.history li { margin: 12px 0; }.history small { display: block; overflow-wrap: anywhere; }
</style>
