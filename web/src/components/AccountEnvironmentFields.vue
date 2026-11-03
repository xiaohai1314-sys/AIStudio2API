<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { useI18n } from '@/i18n'
import type { AccountLoginInput, EnvironmentCheckResult } from '@/types'

// environment 是账户的代理、语言与时区
const environment = defineModel<AccountLoginInput>({ required: true })
const { t } = useI18n()

const checking = ref(false)
const downloading = ref(false)
const failure = ref('')
const result = ref<EnvironmentCheckResult | null>(null)

// 环境改动后旧结果不再成立
watch(
  () => [environment.value.proxy, environment.value.locale, environment.value.timezone],
  () => {
    result.value = null
    failure.value = ''
  },
)

// hasSuggestion 表示检查结果里有与当前填写不同的建议值
const hasSuggestion = computed(() => {
  const check = result.value
  if (check === null) return false
  const locale = check.suggested_locale ?? ''
  const timezone = check.suggested_timezone ?? ''
  return (
    (locale !== '' && locale !== environment.value.locale) ||
    (timezone !== '' && timezone !== environment.value.timezone)
  )
})

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

async function runCheck(): Promise<void> {
  if (checking.value) return
  checking.value = true
  failure.value = ''
  try {
    result.value = await api.environmentCheck({
      proxy: environment.value.proxy,
      locale: environment.value.locale,
      timezone: environment.value.timezone,
    })
  } catch (error) {
    failure.value = describe(error)
  } finally {
    checking.value = false
  }
}

// downloadGeoIP 安装本地 GeoIP 数据库，成功后重新检查
async function downloadGeoIP(): Promise<void> {
  if (downloading.value) return
  downloading.value = true
  failure.value = ''
  try {
    await api.updateGeoIP()
    await runCheck()
  } catch (error) {
    failure.value = describe(error)
  } finally {
    downloading.value = false
  }
}

// applySuggestion 把建议值写回表单，由用户提交后才保存
function applySuggestion(): void {
  const check = result.value
  if (check === null) return
  if (check.suggested_locale) environment.value.locale = check.suggested_locale
  if (check.suggested_timezone) environment.value.timezone = check.suggested_timezone
  void runCheck()
}
</script>

<template>
  <label class="block">
    <span class="mb-1 block text-sm font-medium text-gray-400">{{ t('accounts.proxy') }}</span>
    <input
      v-model.trim="environment.proxy"
      class="w-full rounded border border-line bg-canvas px-3 py-2 text-white transition focus:border-blue-500 focus:outline-none"
      placeholder="socks5://127.0.0.1:1080"
    />
  </label>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
    <label class="block">
      <span class="mb-1 block text-sm font-medium text-gray-400">{{ t('accounts.locale') }}</span>
      <input
        v-model.trim="environment.locale"
        class="w-full rounded border border-line bg-canvas px-3 py-2 text-white transition focus:border-blue-500 focus:outline-none"
        required
      />
    </label>
    <label class="block">
      <span class="mb-1 block text-sm font-medium text-gray-400">{{ t('accounts.timezone') }}</span>
      <input
        v-model.trim="environment.timezone"
        class="w-full rounded border border-line bg-canvas px-3 py-2 text-white transition focus:border-blue-500 focus:outline-none"
        required
      />
    </label>
  </div>
  <div class="space-y-2">
    <div class="flex items-center justify-between gap-2">
      <button
        class="rounded bg-raised px-3 py-1.5 text-sm text-gray-300 transition hover:bg-line disabled:opacity-50"
        type="button"
        :disabled="checking"
        :aria-busy="checking"
        @click="runCheck"
      >
        {{ checking ? t('accounts.envCheckRunning') : t('accounts.envCheck') }}
      </button>
      <span
        v-if="result?.exit_ip"
        class="min-w-0 truncate font-mono text-xs text-gray-500"
        :title="`${t('accounts.envExit')} ${result.exit_ip}${result.country ? ` · ${result.country}` : ''}`"
      >
        {{ t('accounts.envExit') }} {{ result.exit_ip
        }}<template v-if="result.country"> · {{ result.country }}</template>
      </span>
    </div>
    <p v-if="failure" class="rounded border border-red-900 bg-red-950/40 p-2 text-xs text-red-300">
      {{ failure }}
    </p>
    <template v-else-if="result">
      <ul
        v-if="result.warnings.length > 0"
        class="space-y-1 rounded border border-amber-900 bg-amber-950/40 p-2 text-xs text-amber-200"
      >
        <li v-for="warning in result.warnings" :key="warning.code">{{ warning.message }}</li>
      </ul>
      <p v-else class="rounded border border-line bg-canvas p-2 text-xs text-green-400">
        {{ t('accounts.envNoWarnings') }}
      </p>
      <p v-if="result.error" class="text-xs text-red-300">{{ result.error }}</p>
      <div
        v-if="!result.geoip_available"
        class="flex items-center justify-between gap-2 rounded border border-line bg-canvas p-2 text-xs text-gray-400"
      >
        <span>{{ t('accounts.envGeoIPMissing') }}</span>
        <button
          class="shrink-0 text-blue-400 transition hover:text-blue-300 disabled:opacity-50"
          type="button"
          :disabled="downloading"
          :aria-busy="downloading"
          @click="downloadGeoIP"
        >
          {{ t('accounts.envGeoIPDownload') }}
        </button>
      </div>
      <button
        v-if="hasSuggestion"
        class="text-xs text-blue-400 transition hover:text-blue-300"
        type="button"
        @click="applySuggestion"
      >
        {{ t('accounts.envApply') }}
      </button>
    </template>
  </div>
</template>
