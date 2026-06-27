/**
 * 用户信息管理类
 * 使用数据库存储用户信息，通过 HTTP API 访问
 * 提供内存缓存以减少 API 调用
 */

import { api, type UserInfo, type SaveUserInfoParams, type AuthUser } from './api'
import { authManager } from './AuthManager'

// 缓存有效期（5分钟）
const CACHE_DURATION_MS = 5 * 60 * 1000

// 内存缓存
let cachedUserInfo: UserInfo | null = null
let cacheTimestamp: number = 0

class UserInfoManager {
  /**
   * 获取用户信息（优先从缓存获取）
   * @returns 用户信息，如果不存在返回 null
   */
  async getUserInfo(): Promise<UserInfo | null> {
    const authUser = authManager.getCurrentUser()
    if (authUser) {
      return this.authUserToUserInfo(authUser)
    }

    // 检查缓存是否有效
    if (cachedUserInfo && !this.isCacheExpired()) {
      return cachedUserInfo
    }

    try {
      const userInfo = await api.getUserInfo()
      if (userInfo) {
        cachedUserInfo = userInfo
        cacheTimestamp = Date.now()
      }
      return userInfo
    } catch (error) {
      console.error('Failed to get user info from server:', error)
      // 如果 API 调用失败但有缓存，返回缓存的数据
      if (cachedUserInfo) {
        return cachedUserInfo
      }
      return null
    }
  }

  /**
   * 保存用户信息
   * @param info 用户信息
   */
  async saveUserInfo(info: SaveUserInfoParams): Promise<UserInfo | null> {
    try {
      const userInfo = await api.saveUserInfo(info)
      // 更新缓存
      cachedUserInfo = userInfo
      cacheTimestamp = Date.now()
      return userInfo
    } catch (error) {
      console.error('Failed to save user info to server:', error)
      throw error
    }
  }

  /**
   * 清除缓存
   */
  clearCache(): void {
    cachedUserInfo = null
    cacheTimestamp = 0
  }

  /**
   * 检查用户信息是否存在
   */
  async hasValidUserInfo(): Promise<boolean> {
    const userInfo = await this.getUserInfo()
    return userInfo !== null
  }

  /**
   * 同步获取缓存的用户信息（不发起网络请求）
   * 用于快速访问，如果缓存不存在则返回 null
   */
  getCachedUserInfo(): UserInfo | null {
    if (cachedUserInfo && !this.isCacheExpired()) {
      return cachedUserInfo
    }
    return null
  }

  /**
   * 检查是否有缓存的用户信息（同步方法）
   */
  hasCachedUserInfo(): boolean {
    return cachedUserInfo !== null && !this.isCacheExpired()
  }

  /**
   * 检查缓存是否已过期
   */
  private isCacheExpired(): boolean {
    return Date.now() - cacheTimestamp > CACHE_DURATION_MS
  }

  private authUserToUserInfo(user: AuthUser): UserInfo {
    const now = new Date().toISOString()
    return {
      id: user.id,
      company_name: user.user_unit,
      user_name: user.display_name,
      department: user.user_department,
      ip: '',
      mac_address: '',
      work_address: null,
      phone: user.phone,
      create_time: now,
      update_time: now,
    }
  }

  /**
   * 预加载用户信息到缓存
   * 在应用启动时调用
   */
  async preload(): Promise<void> {
    try {
      await this.getUserInfo()
    } catch (error) {
      console.error('Failed to preload user info:', error)
    }
  }
}

export const userInfoManager = new UserInfoManager()

// 导出类型以便其他组件使用
export type { UserInfo, SaveUserInfoParams }
