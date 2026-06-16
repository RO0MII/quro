import { createInterface } from 'readline'
import { writeFileSync, existsSync, readFileSync } from 'fs'
import { join, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const ENV_PATH = join(__dirname, '..', '.env.local')

function ask(query) {
  const rl = createInterface({ input: process.stdin, output: process.stdout })
  return new Promise((resolve) => rl.question(query, (a) => { rl.close(); resolve(a.trim()) }))
}

async function main() {
  console.log('')
  console.log('  \x1b[37m╔══════════════════════════════════════╗\x1b[0m')
  console.log('  \x1b[37m║        Quro Panel Setup              ║\x1b[0m')
  console.log('  \x1b[37m╚══════════════════════════════════════╝\x1b[0m')
  console.log('')

  let config = {}
  if (existsSync(ENV_PATH)) {
    const existing = readFileSync(ENV_PATH, 'utf-8')
    existing.split('\n').forEach(line => {
      const [k, ...v] = line.split('=')
      if (k) config[k.trim()] = v.join('=').trim()
    })
    console.log('  \x1b[90mExisting config found at .env.local\x1b[0m\n')
  }

  const panelUrl = await ask(`  Panel URL [${config.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}]: `) || config.NEXT_PUBLIC_API_URL || 'http://localhost:8080'
  const apiUrl = await ask(`  API URL [${config.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}]: `) || config.NEXT_PUBLIC_API_URL || 'http://localhost:8080'

  console.log('')
  console.log('  \x1b[37m── Admin Account ──────────────────────\x1b[0m\n')

  const username = await ask('  Username: ')
  if (!username || username.length < 3) {
    console.log('  \x1b[31mError: Username must be at least 3 characters\x1b[0m')
    process.exit(1)
  }

  const email = await ask('  Email: ')
  if (!email || !email.includes('@')) {
    console.log('  \x1b[31mError: Valid email required\x1b[0m')
    process.exit(1)
  }

  const password = await ask('  Password: ')
  if (!password || password.length < 8) {
    console.log('  \x1b[31mError: Password must be at least 8 characters\x1b[0m')
    process.exit(1)
  }

  const envContent = [
    `NEXT_PUBLIC_API_URL=${apiUrl}`,
    `NEXT_PUBLIC_WS_URL=${apiUrl.replace(/^http/, 'ws')}`,
    '',
    `# Admin account created during setup`,
    `# Username: ${username}`,
    `# Email: ${email}`,
  ].join('\n')

  writeFileSync(ENV_PATH, envContent)
  console.log(`\n  \x1b[32m✓ .env.local saved\x1b[0m`)

  console.log(`  \x1b[90mAdmin account is created automatically during Docker setup.\x1b[0m`)
  console.log(`  \x1b[90mUse the manage-users.sh script to add more users.\x1b[0m`)

  console.log('')
  console.log('  \x1b[37m── Setup complete ─────────────────────\x1b[0m')
  console.log('')
  console.log(`  \x1b[90m  Run: PORT=8080 HOSTNAME=0.0.0.0 npx next start\x1b[0m`)
  console.log(`  \x1b[90m  Open: http://localhost:8080\x1b[0m`)
  console.log('')
}

main().catch((e) => {
  console.error('  \x1b[31mError:', e.message, '\x1b[0m')
  process.exit(1)
})
