<script setup lang="ts">
// 自有文件本地归档（演示页面，仅展示用，无实际逻辑）
import { ref, computed } from 'vue'

interface ArchiveDir {
  name: string
  level: string
  type: 'system' | 'custom'
  privacy?: boolean
  count: number
}

// 系统固定的 4 个本机隐藏目录
const systemDirs = ref<ArchiveDir[]>([
  { name: '个人保密夹', level: '核心级', type: 'system', count: 32 },
  { name: '个人档案夹', level: '重要级', type: 'system', count: 21 },
  { name: '个人资料夹', level: '一般级', type: 'system', count: 45 },
  { name: '个人隐私文件夹', level: '隐私级', type: 'system', privacy: true, count: 8 },
])
const customDirs = ref<ArchiveDir[]>([])

const allDirs = computed(() => [...systemDirs.value, ...customDirs.value])

const newDirName = ref('')
const newDirLevel = ref('核心级')
const levels = ['核心级', '重要级', '一般级', '隐私级']

const archiveTime = ref('未执行')

const snackbar = ref(false)
const snackbarText = ref('')
const showTip = (text: string) => {
  snackbarText.value = text
  snackbar.value = true
}

const nowText = () => new Date().toLocaleString('zh-CN')

const addCustomDir = () => {
  const name = newDirName.value.trim()
  if (!name) {
    showTip('请输入目录名称')
    return
  }
  customDirs.value.push({ name, level: newDirLevel.value, type: 'custom', count: Math.floor(Math.random() * 30 + 4) })
  newDirName.value = ''
}

const deleteCustomDir = (name: string) => {
  customDirs.value = customDirs.value.filter(d => d.name !== name)
}

const doArchive = () => {
  archiveTime.value = nowText()
  showTip('本机文件已自动归档到 4 个系统隐藏目录 + 自定义目录（演示）')
}

const levelColor = (level: string) => {
  if (level === '核心级' || level === '隐私级') return 'error'
  if (level === '重要级') return 'warning'
  return 'success'
}

const headers = [
  { title: '归档目录', key: 'name', sortable: false },
  { title: '级别', key: 'level', sortable: false, width: '110px' },
  { title: '目录类型', key: 'kind', sortable: false, width: '120px' },
  { title: '文件数', key: 'count', sortable: false, width: '100px' },
  { title: '操作', key: 'actions', sortable: false, width: '160px' },
]
</script>

<template>
  <div>
    <!-- 标题和提示 -->
    <v-card class="mb-4" elevation="1">
      <v-card-title class="d-flex align-center">
        <v-icon class="mr-2">mdi-folder-lock-outline</v-icon>
        自有文件本地归档
      </v-card-title>
      <v-card-text>
        <div class="text-body-2 text-grey">
          系统固定 4 个本机隐藏目录：个人保密夹（核心）、档案夹（重要）、资料夹（一般）、隐私文件夹（隐私保），同时支持自定义归档目录。
        </div>
      </v-card-text>
    </v-card>

    <!-- 新增自定义目录 + 开始归档 -->
    <v-card class="mb-4" elevation="1">
      <v-card-text>
        <v-row align="center">
          <v-col cols="12" md="5">
            <v-text-field
              v-model="newDirName"
              label="新增自定义目录"
              placeholder="输入目录名称"
              variant="outlined"
              density="compact"
              hide-details
            />
          </v-col>
          <v-col cols="6" md="3">
            <v-select
              v-model="newDirLevel"
              :items="levels"
              label="级别"
              variant="outlined"
              density="compact"
              hide-details
            />
          </v-col>
          <v-col cols="6" md="4" class="d-flex align-center ga-2">
            <v-btn color="primary" variant="tonal" prepend-icon="mdi-folder-plus-outline" @click="addCustomDir">
              添加目录
            </v-btn>
            <v-btn color="primary" prepend-icon="mdi-play" @click="doArchive">
              开始自动归档
            </v-btn>
          </v-col>
        </v-row>
        <div class="text-body-2 text-grey mt-2">
          归档时间：<b>{{ archiveTime }}</b>
        </div>
      </v-card-text>
    </v-card>

    <!-- 归档目录表 -->
    <v-card elevation="1">
      <v-data-table
        :headers="headers"
        :items="allDirs"
        item-value="name"
        hide-default-footer
        :items-per-page="-1"
      >
        <template v-slot:item.name="{ item }">
          <div class="d-flex align-center">
            <v-icon size="small" class="mr-2" :color="item.privacy ? 'error' : 'amber-darken-2'">
              {{ item.privacy ? 'mdi-folder-key-outline' : 'mdi-folder-outline' }}
            </v-icon>
            {{ item.name }}
            <v-chip v-if="item.privacy" size="x-small" color="error" variant="tonal" class="ml-2">隐私保</v-chip>
          </div>
        </template>
        <template v-slot:item.level="{ item }">
          <v-chip size="small" variant="tonal" :color="levelColor(item.level)">{{ item.level }}</v-chip>
        </template>
        <template v-slot:item.kind="{ item }">
          <v-chip size="small" variant="text" color="grey">
            {{ item.type === 'system' ? '隐藏目录' : '自定义目录' }}
          </v-chip>
        </template>
        <template v-slot:item.actions="{ item }">
          <v-btn size="small" variant="text" color="primary">查看</v-btn>
          <v-btn
            v-if="item.type === 'custom'"
            size="small"
            variant="text"
            color="error"
            @click="deleteCustomDir(item.name)"
          >
            删除
          </v-btn>
        </template>
      </v-data-table>
    </v-card>

    <v-snackbar v-model="snackbar" color="success" :timeout="2500">{{ snackbarText }}</v-snackbar>
  </div>
</template>
