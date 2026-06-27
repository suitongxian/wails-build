/**
 * 选项卡状态管理器
 */

const STORAGE_KEY = 'active_tab'

/**
 * 保存选项卡状态
 */
export function saveTabState(tabValue: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, tabValue)
  } catch (e) {
    console.error('Failed to save tab state:', e)
  }
}

/**
 * 加载选项卡状态
 */
export function loadTabState(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch (e) {
    console.error('Failed to load tab state:', e)
    return null
  }
}
