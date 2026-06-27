<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type UserInfo } from '@/services/api'

// 状态
const loading = ref(false)
const saving = ref(false)
const snackbar = ref(false)
const snackbarText = ref('')
const snackbarColor = ref('success')

// 表单数据
const companyName = ref('')
const userName = ref('')
const department = ref('')
const phone = ref('')

// 加载用户信息
const loadUserInfo = async () => {
  loading.value = true
  try {
    const userInfo = await api.getUserInfo()
    if (userInfo) {
      companyName.value = userInfo.company_name || ''
      userName.value = userInfo.user_name || ''
      department.value = userInfo.department || ''
      phone.value = userInfo.phone || ''
    }
  } catch (error) {
    showSnackbar('加载用户信息失败', 'error')
    console.error('Failed to load user info:', error)
  } finally {
    loading.value = false
  }
}

// 保存用户信息
const saveUserInfo = async () => {
  // 验证必填字段
  if (!companyName.value.trim()) {
    showSnackbar('请填写单位名称', 'warning')
    return
  }
  if (!userName.value.trim()) {
    showSnackbar('请填写用户姓名', 'warning')
    return
  }
  if (!department.value.trim()) {
    showSnackbar('请填写所属部门', 'warning')
    return
  }

  saving.value = true
  try {
    await api.saveUserInfo({
      company_name: companyName.value.trim(),
      user_name: userName.value.trim(),
      department: department.value.trim(),
      phone: phone.value.trim() || null,
    })
    showSnackbar('用户信息已保存', 'success')
  } catch (error) {
    showSnackbar('保存用户信息失败', 'error')
    console.error('Failed to save user info:', error)
  } finally {
    saving.value = false
  }
}

// 显示提示
const showSnackbar = (text: string, color: string) => {
  snackbarText.value = text
  snackbarColor.value = color
  snackbar.value = true
}

// 组件挂载时加载用户信息
onMounted(() => {
  loadUserInfo()
})
</script>

<template>
  <div>
    <v-card elevation="1">
      <v-card-title>
        <v-icon class="mr-2">mdi-account</v-icon>
        用户信息
      </v-card-title>

      <v-card-text>
        <v-skeleton-loader v-if="loading" type="article" />

        <v-form v-else>
          <v-row>
            <v-col cols="12" md="6">
              <v-text-field
                v-model="companyName"
                label="单位名称"
                placeholder="请输入单位名称"
                variant="outlined"
                :rules="[v => !!v || '单位名称为必填项']"
                required
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-text-field
                v-model="userName"
                label="用户姓名"
                placeholder="请输入用户姓名"
                variant="outlined"
                :rules="[v => !!v || '用户姓名为必填项']"
                required
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-text-field
                v-model="department"
                label="所属部门"
                placeholder="请输入所属部门"
                variant="outlined"
                :rules="[v => !!v || '所属部门为必填项']"
                required
              />
            </v-col>

            <v-col cols="12" md="6">
              <v-text-field
                v-model="phone"
                label="联系方式"
                placeholder="请输入联系方式（选填）"
                variant="outlined"
              />
            </v-col>
          </v-row>
        </v-form>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn
          variant="outlined"
          @click="loadUserInfo"
          :disabled="loading || saving"
        >
          重置
        </v-btn>
        <v-btn
          color="primary"
          @click="saveUserInfo"
          :loading="saving"
          :disabled="loading"
        >
          保存
        </v-btn>
      </v-card-actions>
    </v-card>

    <!-- 提示消息 -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" :timeout="3000">
      {{ snackbarText }}
    </v-snackbar>
  </div>
</template>
