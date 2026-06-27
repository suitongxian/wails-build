import { api, type AuthSession, type AuthUser, type LoginParams, type RegisterParams } from './api'

let cachedSession: AuthSession | null = null

function hasAuthenticatedUser(session: AuthSession | null): session is AuthSession & { token: string; user: AuthUser } {
  return Boolean(session?.token && session?.user)
}

class AuthManager {
  async getSession(force = false): Promise<AuthSession | null> {
    if (cachedSession && !force) {
      return cachedSession
    }

    const session = await api.getAuthSession()
    cachedSession = hasAuthenticatedUser(session) || session.authenticated ? session : null
    return cachedSession
  }

  async login(params: LoginParams): Promise<AuthSession> {
    const session = await api.login(params)
    cachedSession = session
    return session
  }

  async register(params: RegisterParams): Promise<AuthSession> {
    const session = await api.register(params)
    cachedSession = session
    return session
  }

  async logout(): Promise<void> {
    await api.logout()
    this.clearCache()
  }

  getCurrentUser(): AuthUser | null {
    return cachedSession?.user || null
  }

  isAuthenticated(): boolean {
    return hasAuthenticatedUser(cachedSession)
  }

  clearCache(): void {
    cachedSession = null
  }
}

export const authManager = new AuthManager()
export type { AuthSession, AuthUser, LoginParams, RegisterParams }
