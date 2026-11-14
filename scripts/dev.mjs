import { spawn } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

export function runServices(services, { shutdownTimeoutMs = 5000 } = {}) {
  if (process.platform === 'win32') {
    throw new Error('开发启动器需要 macOS、Linux 或 WSL；Windows 可分别启动前后端。')
  }
  return new Promise(resolveResult => {
    const children = []
    let stopping = false
    let forced = false
    let exitCode = 0
    let forceTimer
    let pollTimer

    const signalGroup = (child, signal) => {
      if (!child.pid) return
      try {
        process.kill(-child.pid, signal)
      } catch (error) {
        if (error.code !== 'ESRCH') throw error
      }
    }
    const groupExists = child => {
      if (!child.pid) return false
      try {
        process.kill(-child.pid, 0)
        return true
      } catch (error) {
        if (error.code === 'ESRCH') return false
        throw error
      }
    }
    const finishIfStopped = () => {
      if (!stopping || children.some(record => !record.exited)) return
      if (!forced && children.some(record => groupExists(record.child))) return
      clearTimeout(forceTimer)
      clearInterval(pollTimer)
      process.off('SIGINT', onInterrupt)
      process.off('SIGTERM', onTerminate)
      resolveResult(exitCode)
    }
    const stop = code => {
      if (stopping) return
      stopping = true
      exitCode = code
      for (const { child } of children) signalGroup(child, 'SIGTERM')
      forceTimer = setTimeout(() => {
        forced = true
        for (const { child } of children) signalGroup(child, 'SIGKILL')
        finishIfStopped()
      }, shutdownTimeoutMs)
      pollTimer = setInterval(finishIfStopped, 25)
      finishIfStopped()
    }
    const onInterrupt = () => stop(130)
    const onTerminate = () => stop(143)
    process.on('SIGINT', onInterrupt)
    process.on('SIGTERM', onTerminate)

    for (const service of services) {
      const child = spawn(service.command, service.args, {
        cwd: service.cwd ?? projectRoot,
        env: { ...process.env, ...service.env },
        detached: true,
        stdio: 'inherit',
      })
      const record = { child, exited: false }
      children.push(record)
      child.once('error', error => {
        record.exited = true
        console.error(`${service.name} 启动失败：${error.message}`)
        stop(1)
        finishIfStopped()
      })
      child.once('exit', (code, signal) => {
        record.exited = true
        if (!stopping) {
          console.error(`${service.name} 已退出${signal ? ` (${signal})` : ` (${code})`}，正在停止其他服务。`)
          stop(code ?? 1)
        }
        finishIfStopped()
      })
    }
    if (children.length === 0) stop(0)
  })
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  console.log('正在启动前后端；Ctrl+C 会停止所有开发进程。')
  process.exitCode = await runServices([
    { name: '后端', command: 'go', args: ['run', '-tags', 'fts5', './cmd/server/main.go'], cwd: resolve(projectRoot, 'backend'), env: { CGO_ENABLED: '1' } },
    { name: '前端', command: 'npm', args: ['run', 'dev'], cwd: resolve(projectRoot, 'frontend') },
  ])
}
