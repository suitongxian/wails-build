<script setup lang="ts">
// 本地档案同步上传（演示页面，仅展示用，无实际逻辑）
import { ref } from 'vue'

const progress = ref(45)
const syncing = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

const startSync = () => {
  if (syncing.value) return
  syncing.value = true
  progress.value = 0
  timer = setInterval(() => {
    progress.value = Math.min(100, progress.value + 10)
    if (progress.value >= 100 && timer) {
      clearInterval(timer)
      timer = null
      syncing.value = false
    }
  }, 300)
}
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-cloud-upload-outline</v-icon>
        本地档案同步上传
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          将本地已归档文件安全上传至单位服务器，支持断点续传、校验。
        </div>
      </v-card-text>
    </v-card>

    <!-- 同步操作 -->
    <v-card elevation="1">
      <v-card-text>
        <v-btn color="primary" prepend-icon="mdi-cloud-upload" :loading="syncing" @click="startSync">
          开始同步
        </v-btn>
        <div class="mt-4">
          <div class="d-flex justify-space-between text-body-2 text-grey mb-1">
            <span>同步进度</span>
            <span>{{ progress }}%</span>
          </div>
          <v-progress-linear
            :model-value="progress"
            color="success"
            height="10"
            rounded
          />
        </div>
      </v-card-text>
    </v-card>
  </div>
</template>
