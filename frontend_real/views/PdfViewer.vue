<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const loading = ref(true)
const error = ref(false)

// 缩放控制
const scale = ref(1)
const minScale = 0.5
const maxScale = 3

// 从路由参数获取 base64 编码的 PDF 数据
const pdfData = computed(() => {
  return route.query.data as string
})

const pdfUrl = ref<string | null>(null)

onMounted(() => {
  if (!pdfData.value) {
    error.value = true
    loading.value = false
    return
  }

  try {
    // 将 base64 数据转换为 blob URL
    const byteCharacters = atob(pdfData.value)
    const byteNumbers = new Array(byteCharacters.length)
    for (let i = 0; i < byteCharacters.length; i++) {
      byteNumbers[i] = byteCharacters.charCodeAt(i)
    }
    const byteArray = new Uint8Array(byteNumbers)
    const blob = new Blob([byteArray], { type: 'application/pdf' })
    pdfUrl.value = URL.createObjectURL(blob)
    loading.value = false

    // 设置默认缩放为 1.2 倍
    scale.value = 1.2
  } catch (e) {
    error.value = true
    loading.value = false
  }
})

// 组件卸载时释放 URL
import { onUnmounted } from 'vue'
onUnmounted(() => {
  if (pdfUrl.value) {
    URL.revokeObjectURL(pdfUrl.value)
  }
})

// 缩放控制函数
const zoomIn = () => {
  if (scale.value < maxScale) {
    scale.value = Math.min(scale.value + 0.2, maxScale)
  }
}

const zoomOut = () => {
  if (scale.value > minScale) {
    scale.value = Math.max(scale.value - 0.2, minScale)
  }
}

const resetZoom = () => {
  scale.value = 1.2
}

const closeWindow = () => {
  window.close()
}

// 计算缩放样式
const scaleStyle = computed(() => {
  return {
    transform: `scale(${scale.value})`,
    transformOrigin: 'top center',
    width: `${100 / scale.value}%`,
    height: `${100 / scale.value}%`
  }
})
</script>

<template>
  <div class="pdf-viewer-container">
    <v-app>
      <!-- 工具栏 -->
      <v-app-bar elevation="1" color="white" height="56">
        <v-btn icon @click="closeWindow">
          <v-icon>mdi-close</v-icon>
        </v-btn>
        <v-app-bar-title class="text-body-2">PDF 在线预览</v-app-bar-title>
        <v-spacer />

        <!-- 缩放控制 -->
        <div class="d-flex align-center gap-2 mr-4">
          <v-btn icon size="small" variant="text" @click="zoomOut" :disabled="scale <= minScale">
            <v-icon>mdi-minus</v-icon>
          </v-btn>
          <span class="text-body-2" style="min-width: 60px; text-align: center">
            {{ Math.round(scale * 100) }}%
          </span>
          <v-btn icon size="small" variant="text" @click="zoomIn" :disabled="scale >= maxScale">
            <v-icon>mdi-plus</v-icon>
          </v-btn>
          <v-btn size="small" variant="text" @click="resetZoom">
            <v-icon start size="small">mdi-refresh</v-icon>
            重置
          </v-btn>
        </div>
      </v-app-bar>

      <!-- PDF 显示区域 -->
      <v-main class="bg-grey-lighten-4">
        <v-container class="pdf-container pa-0">
          <!-- 加载中 -->
          <div v-if="loading" class="loading-overlay">
            <v-progress-circular indeterminate size="64" color="primary" />
            <div class="mt-4">加载中...</div>
          </div>

          <!-- 错误提示 -->
          <div v-else-if="error" class="error-overlay">
            <div class="text-center">
              <v-icon size="64" color="error">mdi-file-alert-outline</v-icon>
              <div class="mt-4">加载 PDF 失败</div>
              <v-btn variant="text" color="primary" class="mt-4" @click="closeWindow">关闭</v-btn>
            </div>
          </div>

          <!-- PDF 缩放容器 -->
          <div v-else class="pdf-scale-container">
            <iframe
              :src="`${pdfUrl}#toolbar=0&navpanes=0`"
              class="pdf-iframe"
              type="application/pdf"
              :style="scaleStyle"
            />
          </div>
        </v-container>
      </v-main>
    </v-app>
  </div>
</template>

<style scoped>
.pdf-viewer-container {
  width: 100vw;
  height: 100vh;
  overflow: hidden;
}

.pdf-container {
  width: 100%;
  height: calc(100vh - 56px);
  padding: 0;
  position: relative;
}

/* 缩放容器 - 提供足够的空间容纳缩放后的内容 */
.pdf-scale-container {
  width: 100%;
  height: 100%;
  display: flex;
  justify-content: center;
  overflow: auto;
  padding: 20px;
}

.pdf-iframe {
  border: none;
  display: block;
  background: white;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  transition: transform 0.2s ease;
  min-width: 800px;
  min-height: 600px;
}

.loading-overlay,
.error-overlay {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  width: 100%;
  position: absolute;
  top: 0;
  left: 0;
}

/* 滚动条样式 */
.pdf-scale-container::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

.pdf-scale-container::-webkit-scrollbar-track {
  background: #f1f1f1;
  border-radius: 4px;
}

.pdf-scale-container::-webkit-scrollbar-thumb {
  background: #888;
  border-radius: 4px;
}

.pdf-scale-container::-webkit-scrollbar-thumb:hover {
  background: #555;
}
</style>
