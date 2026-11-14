import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

const launcherUrl = new URL('./dev.mjs', import.meta.url).href
const fixture = `
import { spawn } from 'node:child_process'
import { existsSync, writeFileSync } from 'node:fs'
const descendant = spawn(process.execPath, ['-e', "process.on('SIGTERM', () => {});setInterval(() => {}, 1000)"], { stdio: 'ignore' })
process.on('SIGTERM', () => {})
writeFileSync(process.env.MARKER, JSON.stringify({ pid: process.pid, descendant: descendant.pid }))
if (process.env.FAIL_AFTER) {
  while (!existsSync(process.env.FAIL_AFTER)) await new Promise(resolve => setTimeout(resolve, 10))
  process.exit(7)
}
setInterval(() => {}, 1000)
`

async function until(check) {
  const deadline = Date.now() + 4000
  while (Date.now() < deadline) {
    if (await check()) return
    await new Promise(resolve => setTimeout(resolve, 10))
  }
  throw new Error('fixture did not reach the expected process state')
}

async function running(pid) {
  try {
    process.kill(pid, 0)
    if (process.platform === 'linux') {
      const status = await readFile(`/proc/${pid}/stat`, 'utf8')
      if (status.slice(status.lastIndexOf(')') + 2).startsWith('Z')) return false
    }
    return true
  } catch (error) {
    if (error.code === 'ESRCH' || error.code === 'ENOENT') return false
    throw error
  }
}

async function launch(t, fail = false, missing = false) {
  const directory = await mkdtemp(join(tmpdir(), 'one-mail dev test '))
  const fixturePath = join(directory, 'service.mjs')
  await writeFile(fixturePath, fixture)
  const markers = [join(directory, 'first.json'), join(directory, 'second.json')]
  const services = markers.map((marker, index) => ({
    name: `fixture-${index}`,
    command: missing && index === 1 ? join(directory, 'missing-command') : process.execPath,
    args: [fixturePath],
    env: { MARKER: marker, FAIL_AFTER: fail && index === 1 ? markers[0] : '' },
  }))
  const child = spawn(process.execPath, [
    '--input-type=module', '--eval',
    `const { runServices } = await import(${JSON.stringify(launcherUrl)}); process.exitCode = await runServices(JSON.parse(process.argv[1]), { shutdownTimeoutMs: 200 });`,
    JSON.stringify(services),
  ], { stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout.resume()
  child.stderr.resume()
  const closed = once(child, 'close')
  t.after(async () => {
    child.kill('SIGKILL')
    for (const marker of markers) {
      try {
        const record = JSON.parse(await readFile(marker, 'utf8'))
        try { process.kill(-record.pid, 'SIGKILL') } catch (error) { if (error.code !== 'ESRCH') throw error }
      } catch (error) {
        if (error.code !== 'ENOENT') throw error
      }
    }
    await rm(directory, { recursive: true, force: true })
  })
  return { child, closed, markers }
}

for (const signal of ['SIGINT', 'SIGTERM']) {
  test(`${signal} stops both services and their descendants`, { timeout: 10000 }, async t => {
    const { child, closed, markers } = await launch(t)
    const records = []
    for (const marker of markers) {
      await until(async () => { try { records.push(JSON.parse(await readFile(marker, 'utf8'))); return true } catch { return false } })
    }
    child.kill(signal)
    assert.equal((await closed)[0], signal === 'SIGINT' ? 130 : 143)
    for (const record of records) {
      await until(async () => !await running(record.pid) && !await running(record.descendant))
    }
  })
}

test('a service failure stops its peer and retains the failure status', { timeout: 10000 }, async t => {
  const { closed, markers } = await launch(t, true)
  assert.equal((await closed)[0], 7)
  for (const marker of markers) {
    const record = JSON.parse(await readFile(marker, 'utf8'))
    await until(async () => !await running(record.pid) && !await running(record.descendant))
  }
})

test('a missing executable stops startup without leaving its peer', { timeout: 10000 }, async t => {
  const { closed } = await launch(t, false, true)
  assert.equal((await closed)[0], 1)
})
