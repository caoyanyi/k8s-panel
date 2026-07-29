import type { FullConfig } from '@playwright/test'
import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { mkdtemp, rm } from 'node:fs/promises'
import { createConnection } from 'node:net'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { once } from 'node:events'

export default async function globalSetup(config: FullConfig) {
  const repositoryRoot = resolve(import.meta.dirname, '../..')
  const targetURL = config.projects[0].use.baseURL as string
  const parsedURL = new URL(targetURL)
  await assertPortAvailable(parsedURL)
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

  const serverBinary = resolve(temporaryDirectory, process.platform === 'win32' ? 'panel.exe' : 'panel')
  const buildResult = spawnSync('go', ['build', '-trimpath', '-o', serverBinary, './cmd/panel'], {
    cwd: repositoryRoot,
    encoding: 'utf8',
  })
  if (buildResult.status !== 0) {
    await rm(temporaryDirectory, { recursive: true, force: true })
    throw new Error(`failed to build E2E server: ${buildResult.stderr}`)
  }

  const child = spawn(serverBinary, [], {
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

async function assertPortAvailable(url: URL) {
  const port = Number(url.port || (url.protocol === 'https:' ? 443 : 80))
  await new Promise<void>((resolvePromise, reject) => {
    const socket = createConnection({ host: url.hostname, port })
    let settled = false
    const finish = (error?: Error) => {
      if (settled) return
      settled = true
      socket.destroy()
      if (error) reject(error)
      else resolvePromise()
    }
    socket.once('connect', () => finish(new Error(`E2E port ${url.hostname}:${port} is already in use`)))
    socket.once('error', () => finish())
    socket.setTimeout(500, () => finish())
  })
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
