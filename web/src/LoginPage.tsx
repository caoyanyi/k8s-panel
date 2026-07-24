import { Boxes, LoaderCircle, LockKeyhole } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { api, errorMessage } from './api'
import type { Principal } from './types'

export function LoginPage({ onAuthenticated }: { onAuthenticated: (principal: Principal) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!username.trim() || !password) {
      setError('请输入用户名和密码')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const principal = await api.post<Principal>('/api/v1/session', { username: username.trim(), password })
      setPassword('')
      onAuthenticated(principal)
    } catch (caught) {
      setPassword('')
      setError(errorMessage(caught))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="login-page">
      <section className="login-panel" aria-labelledby="login-title">
        <div className="login-brand">
          <span className="brand-mark"><Boxes size={24} aria-hidden="true" /></span>
          <span>KubePanel</span>
        </div>
        <div className="login-heading">
          <LockKeyhole size={24} aria-hidden="true" />
          <div>
            <h1 id="login-title">登录 KubePanel</h1>
            <p>集群运维控制台</p>
          </div>
        </div>
        <form onSubmit={submit} noValidate>
          <div className="field">
            <label htmlFor="login-username">用户名</label>
            <input
              id="login-username"
              name="username"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              disabled={submitting}
            />
          </div>
          <div className="field">
            <label htmlFor="login-password">密码</label>
            <input
              id="login-password"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={submitting}
            />
          </div>
          {error && <div className="form-error" role="alert">{error}</div>}
          <button type="submit" className="button button-primary login-button" disabled={submitting}>
            {submitting && <LoaderCircle className="spin" size={17} aria-hidden="true" />}
            登录
          </button>
        </form>
      </section>
    </main>
  )
}
