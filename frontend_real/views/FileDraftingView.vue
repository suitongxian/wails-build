<script setup lang="ts">
// 电子文件起草管理（演示页面，仅展示用，无实际逻辑）
import { ref } from 'vue'

const title = ref('')
const fileType = ref('文档')
const fileTypes = ['文档', '表格', 'PPT']

const snackbar = ref(false)
const snackbarText = ref('')

const showTip = (text: string) => {
  snackbarText.value = text
  snackbar.value = true
}
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-file-edit-outline</v-icon>
        电子文件起草管理
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          新建、编辑、保存、归档电子文件，自动纳入管控体系。
        </div>
      </v-card-text>
    </v-card>

    <!-- 起草表单 -->
    <v-card elevation="1">
      <v-card-text>
        <v-row>
          <v-col cols="12" md="6">
            <v-text-field
              v-model="title"
              label="文件标题"
              placeholder="请输入文件标题"
              variant="outlined"
              density="comfortable"
            />
          </v-col>
          <v-col cols="12" md="6">
            <v-select
              v-model="fileType"
              :items="fileTypes"
              label="文件类型"
              variant="outlined"
              density="comfortable"
            />
          </v-col>
        </v-row>
        <div class="d-flex ga-2 mt-2">
          <v-btn color="primary" prepend-icon="mdi-file-plus-outline" @click="showTip('已新建文件（演示）')">
            新建文件
          </v-btn>
          <v-btn color="primary" variant="tonal" prepend-icon="mdi-archive-arrow-down-outline" @click="showTip('已保存归档（演示）')">
            保存归档
          </v-btn>
        </div>
      </v-card-text>
    </v-card>

    <v-snackbar v-model="snackbar" color="success" :timeout="2000">{{ snackbarText }}</v-snackbar>
  </div>
</template>
