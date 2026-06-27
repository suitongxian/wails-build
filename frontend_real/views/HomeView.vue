<script setup lang="ts">
</script>

<template>
  <div>
    <v-row>
      <v-col cols="12">
        <h1 class="text-h4 mb-4">欢迎使用数据资产扫描系统</h1>
      </v-col>
    </v-row>

    <v-row>
      <v-col cols="12" md="4">
        <v-card>
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-magnify-scan</v-icon>
              扫描任务
            </v-card-title>
          </v-card-item>
          <v-card-text>
            管理和执行数据扫描任务
          </v-card-text>
          <v-card-actions>
            <v-btn color="primary" to="/scan">开始扫描</v-btn>
          </v-card-actions>
        </v-card>
      </v-col>

      <v-col cols="12" md="4">
        <v-card>
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-database</v-icon>
              数据资产
            </v-card-title>
          </v-card-item>
          <v-card-text>
            查看已扫描的数据资产
          </v-card-text>
          <v-card-actions>
            <v-btn color="primary" disabled>查看资产</v-btn>
          </v-card-actions>
        </v-card>
      </v-col>

      <v-col cols="12" md="4">
        <v-card>
          <v-card-item>
            <v-card-title>
              <v-icon start>mdi-cog</v-icon>
              系统设置
            </v-card-title>
          </v-card-item>
          <v-card-text>
            配置系统参数和扫描规则
          </v-card-text>
          <v-card-actions>
            <v-btn color="primary" disabled>进入设置</v-btn>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </div>
</template>

<style scoped>
</style>
