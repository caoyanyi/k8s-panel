import type { FullConfig } from '@playwright/test'
import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { once } from 'node:events'

export default async function globalSetup(config: FullConfig) {
  const repositoryRoot = resolve(import.meta.dirname, '../..')
  const temporaryDirectory = await mkdtemp(resolve(tmpdir(), 'k8s-panel-e2e-'))
  const password = randomBytes(24).toString('base64url')
  const hashResult = spawnSync('go', ['run', './cmd/panelctl', 'hash-password'], {
    cwd: repositoryRoot,
    input: password,
    encoding: 'utf8',
  })
  if (hashResult.status !== 0 || !hashResult.stdout.startsWith('$argon2id$')) {
    await rm(temporaryDirectory, { recursive: true, force: true })
    throw new Error(`failed to generate E2E password hash: ${hashResult.stderr}`)
  }

  const targetURL = config.projects[0].use.baseURL as string
  const parsedURL = new URL(targetURL)
  const child = spawn('go', ['run', './cmd/panel'], {
    cwd: repositoryRoot,
    env: {
      ...process.env,
      PANEL_LISTEN_ADDR: parsedURL.host,
      PANEL_DATA_FILE: resolve(temporaryDirectory, 'panel.json'),
      PANEL_WEB_DIR: resolve(repositoryRoot, 'web/dist'),
      PANEL_ENCRYPTION_KEY: randomBytes(32).toString('base64'),
      PANEL_ADMIN_USERNAME: 'e2e-admin',
      PANEL_ADMIN_PASSWORD_HASH: hashResult.stdout.trim(),
      PANEL_SESSION_TTL: '1h',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const output = captureOutput(child)
  process.env.PANEL_E2E_USERNAME = 'e2e-admin'
  process.env.PANEL_E2E_PASSWORD = password

  try {
    await waitForHealth(`${targetURL}/healthz`, child)
  } catch (error) {
    child.kill('SIGTERM')
    await rm(temporaryDirectory, { recursive: true, force: true })
    throw new Error(`${String(error)}\nserver output:\n${output()}`)
  }

  return async () => {
    child.kill('SIGTERM')
    await Promise.race([once(child, 'exit'), new Promise((resolvePromise) => setTimeout(resolvePromise, 5_000))])
    await rm(temporaryDirectory, { recursive: true, force: true })
  }
}

async function waitForHealth(url: string, child: ChildProcess) {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`E2E server exited with code ${child.exitCode}`)
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // Server has not bound the port yet.
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 150))
  }
  throw new Error('E2E server did not become healthy in 30 seconds')
}

function captureOutput(child: ChildProcess) {
  let buffer = ''
  const append = (chunk: Buffer) => {
    buffer = (buffer + chunk.toString('utf8')).slice(-8_000)
  }
  child.stdout?.on('data', append)
  child.stderr?.on('data', append)
  return () => buffer
}
