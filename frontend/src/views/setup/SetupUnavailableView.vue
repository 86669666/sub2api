<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ state?: 'loading' | 'required' | 'error' }>()
defineEmits<{ retry: [] }>()
const { locale } = useI18n()
const text = computed(() => locale.value.startsWith('zh') ? {
  title: '服务初始化状态', loading: '正在检查服务状态…', required: '此部署使用自动初始化。后端尚未完成初始化，请联系管理员。',
  error: '暂时无法读取服务状态，请稍后重试。', retry: '重试',
} : {
  title: 'Service initialization', loading: 'Checking service status…', required: 'This deployment uses automatic setup. Initialization is incomplete; contact the administrator.',
  error: 'Service status is unavailable. Please try again.', retry: 'Retry',
})
</script>

<template>
  <main data-testid="setup-unavailable" class="flex min-h-screen flex-col items-center justify-center gap-4 p-8" role="status">
    <h1 class="text-xl font-semibold">{{ text.title }}</h1>
    <p>{{ text[props.state || 'required'] }}</p>
    <button v-if="props.state !== 'loading'" class="btn btn-primary" type="button" @click="$emit('retry')">{{ text.retry }}</button>
  </main>
</template>
