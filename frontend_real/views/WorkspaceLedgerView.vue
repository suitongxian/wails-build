<script setup lang="ts">
// 工作文件台账总览（演示页面，仅展示用，无实际逻辑）
import { ref } from 'vue'

const search = ref('')

const headers = [
  { title: '目录', key: 'dir', sortable: true },
  { title: '文件数', key: 'count', sortable: true, width: '120px' },
  { title: '大小', key: 'size', sortable: true, width: '120px' },
  { title: '状态', key: 'status', sortable: false, width: '120px' },
]

const items = ref([
  { dir: '工作文档', count: 126, size: '1.2G', status: '正常' },
  { dir: '项目资料', count: 84, size: '860M', status: '正常' },
  { dir: '归档备份', count: 52, size: '420M', status: '正常' },
  { dir: '临时草稿', count: 18, size: '96M', status: '正常' },
])
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-file-table-outline</v-icon>
        工作文件台账总览
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          展示个人工作空间全部文件台账，支持检索、统计、导出、归档。
        </div>
      </v-card-text>
    </v-card>

    <!-- 搜索栏 + 操作 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="4">
            <v-text-field
              v-model="search"
              prepend-inner-icon="mdi-magnify"
              label="搜索目录名称"
              variant="outlined"
              density="compact"
              hide-details
              clearable
            />
          </v-col>
          <v-col cols="12" md="8" class="d-flex align-center justify-end ga-2">
            <v-btn variant="tonal" color="primary" prepend-icon="mdi-chart-bar">统计</v-btn>
            <v-btn variant="tonal" color="primary" prepend-icon="mdi-file-export-outline">导出</v-btn>
            <v-btn variant="tonal" color="primary" prepend-icon="mdi-archive-arrow-down-outline">归档</v-btn>
          </v-col>
        </v-row>
      </v-card-text>
    </v-card>

    <!-- 台账表格 -->
    <v-card elevation="1">
      <v-data-table
        :headers="headers"
        :items="items"
        item-value="dir"
        hide-default-footer
      >
        <template v-slot:item.dir="{ item }">
          <div class="d-flex align-center">
            <v-icon size="small" class="mr-2" color="amber-darken-2">mdi-folder-outline</v-icon>
            {{ item.dir }}
          </div>
        </template>
        <template v-slot:item.status="{ item }">
          <v-chip size="small" variant="tonal" color="success">{{ item.status }}</v-chip>
        </template>
      </v-data-table>
    </v-card>
  </div>
</template>
