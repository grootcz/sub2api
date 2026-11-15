<template>
  <div class="space-y-6" data-testid="custom-email-provider-settings">
    <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.smtp.custom.baseUrl') }}
        </label>
        <input
          :value="props.baseUrl"
          type="text"
          class="input"
          :placeholder="t('admin.settings.smtp.custom.baseUrlPlaceholder')"
          data-testid="custom-email-base-url"
          @input="emit('update:baseUrl', ($event.target as HTMLInputElement).value)"
        />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.smtp.custom.baseUrlHint') }}
        </p>
      </div>
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.smtp.custom.token') }}
        </label>
        <input
          :value="props.token"
          type="password"
          autocomplete="new-password"
          autocapitalize="off"
          spellcheck="false"
          class="input"
          :placeholder="t('admin.settings.smtp.custom.tokenPlaceholder')"
          data-testid="custom-email-token"
          @keydown="emit('update:tokenEdited', true)"
          @paste="emit('update:tokenEdited', true)"
          @input="emit('update:token', ($event.target as HTMLInputElement).value)"
        />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{
            props.tokenConfigured
              ? t('admin.settings.smtp.custom.tokenConfiguredHint')
              : t('admin.settings.smtp.custom.tokenHint')
          }}
        </p>
      </div>
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.smtp.custom.from') }}
        </label>
        <input
          :value="props.from"
          type="text"
          class="input"
          :placeholder="t('admin.settings.smtp.custom.fromPlaceholder')"
          data-testid="custom-email-from"
          @input="emit('update:from', ($event.target as HTMLInputElement).value)"
        />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.smtp.custom.fromHint') }}
        </p>
      </div>
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.smtp.custom.timeout') }}
        </label>
        <input
          :value="props.timeoutSeconds"
          type="number"
          min="10"
          max="300"
          class="input"
          data-testid="custom-email-timeout"
          @input="onTimeoutInput"
        />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.smtp.custom.timeoutHint') }}
        </p>
      </div>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.settings.smtp.custom.fromNameNote') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps<{
  baseUrl: string
  token: string
  tokenEdited: boolean
  tokenConfigured: boolean
  from: string
  timeoutSeconds: number | string
}>()

const emit = defineEmits<{
  (e: 'update:baseUrl', value: string): void
  (e: 'update:token', value: string): void
  (e: 'update:tokenEdited', value: boolean): void
  (e: 'update:from', value: string): void
  (e: 'update:timeoutSeconds', value: number | string): void
}>()

function onTimeoutInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  emit('update:timeoutSeconds', value === '' ? '' : Number(value))
}
</script>
