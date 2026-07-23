#!/usr/bin/env node

// Runs Mihomo Config rewrites through an external Sub-Store checkout. This is
// development/test tooling only: BoxFleet neither imports nor bundles
// Sub-Store. Input and output are JSON on stdin/stdout so callers can compare
// normalized results without copying Sub-Store implementation code.

const fs = require('node:fs')
const path = require('node:path')
const { createRequire } = require('node:module')

async function main() {
  const repoRoot = path.resolve(__dirname, '..', '..')
  const backend = process.env.SUB_STORE_BACKEND || path.join(repoRoot, 'refs', 'sub-store', 'backend')
  const packageFile = path.join(backend, 'package.json')
  if (!fs.existsSync(packageFile)) {
    throw new Error(`Sub-Store backend not found at ${backend}`)
  }

  process.chdir(backend)
  const subStoreRequire = createRequire(packageFile)
  subStoreRequire('@babel/register')({ cwd: backend, extensions: ['.js'] })

  const subStore = subStoreRequire('./src/core/app').default
  const { ProxyUtils } = subStoreRequire('./src/core/proxy-utils')
  for (const method of ['log', 'info', 'warn', 'error', 'notify']) {
    subStore[method] = () => {}
  }

  const input = JSON.parse(fs.readFileSync(0, 'utf8'))
  let output = {
    $content: input.base,
    $files: [input.base],
    $file: { name: 'boxfleet-compat', type: 'mihomoConfig' }
  }

  try {
    for (const rewrite of input.rewrites) {
      output = await ProxyUtils.process(output, [{
        type: 'Script Operator',
        args: { mode: 'script', content: rewrite.content }
      }])
    }
    process.stdout.write(JSON.stringify({ ok: true, yaml: output.$content }))
  } catch (error) {
    process.stdout.write(JSON.stringify({ ok: false, error: String(error?.message || error) }))
  }
}

main().catch((error) => {
  process.stderr.write(`${error?.stack || error}\n`)
  process.exitCode = 1
})
