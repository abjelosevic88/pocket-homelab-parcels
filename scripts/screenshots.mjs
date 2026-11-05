// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds half a year of parcels):
//
//   DATA_DIR=/tmp/parcels-demo DEMO_DATA=true AUTH_MODE=none PORT=18233 ./pocket-parcels
//   CDP=http://127.0.0.1:19333 BASE=http://127.0.0.1:18233 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// With AUTH_MODE=local it signs in as LOGIN_EMAIL / LOGIN_PASSWORD (default demo@example.com / demodemo1).
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18233'
const shots = process.env.SHOTS !== '0'
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1280, height: 800 },
    deviceScaleFactor: 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const st = await page.evaluate(() => fetch('/api/v1/auth/status').then(r => r.json()))
  if (!st.authenticated) {
    const res = await page.evaluate(([email, password]) => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password }) }).then(r => r.status),
      [process.env.LOGIN_EMAIL || 'demo@example.com', process.env.LOGIN_PASSWORD || 'demodemo1'])
    assert.equal(res, 200, 'demo sign-in')
  }
  return { ctx, page }
}
async function go(page, path, wait = 1200, frozen = true) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  if (frozen) await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
async function api(page, path) { return page.evaluate(p => fetch('/api/v1' + p).then(r => r.json()), path) }
async function noOverflow(page, where) {
  const w = await page.evaluate(() => [document.documentElement.scrollWidth, window.innerWidth])
  assert.ok(w[0] <= w[1] + 1, `${where}: the page is wider than the screen (${w[0]} > ${w[1]})`)
}
/** The parcel with the most steps that is still on its way: the best example. */
async function example(page) {
  const { parcels } = await api(page, '/parcels?view=active')
  const pick = parcels.filter(p => p.tracking_number).sort((a, b) => (b.event_count || 0) - (a.event_count || 0))
  return (pick.find(p => p.status === 'in_transit') || pick[0]).id
}
async function addSheet(page, open) {
  await open()
  await page.waitForSelector('.parcel-modal input')
  await page.fill('.parcel-modal input', 'LP00123456789012')
  await page.waitForTimeout(900)
  assert.ok((await page.textContent('.detect-line')).includes('Cainiao'), 'the add sheet recognises the carrier')
}

// ---- desktop ----
for (const dark of [false, true]) {
  const sfx = dark ? '-dark' : ''
  const { ctx, page } = await session({ dark })
  await go(page, '/')
  assert.ok((await page.content()).includes('Needs you'), 'the overview shows what needs you')
  await shot(page, 'desktop-overview' + sfx)
  const id = await example(page)
  await go(page, `/parcels/${id}`)
  assert.ok((await page.content()).includes('Journey'), 'a parcel shows its journey')
  await shot(page, 'desktop-parcel' + sfx)
  if (!dark) {
    await go(page, '/parcels')
    await shot(page, 'desktop-parcels')
    await go(page, '/parcels?view=delivered', 600)
    await go(page, '/email')
    assert.ok((await page.content()).includes('to look at'), 'the email page lists the finds')
    await shot(page, 'desktop-email')
    await go(page, '/stats')
    await shot(page, 'desktop-stats')
    await go(page, '/settings/apps')
    await shot(page, 'desktop-settings-apps')
    await go(page, '/')
    await addSheet(page, () => page.keyboard.press('n'))
    await shot(page, 'desktop-add')
    for (const p of ['/returns', '/parcels?view=archived', '/settings/tracking', '/settings/notifications', '/settings/email', '/settings/general', '/settings/ai', '/settings/data', '/settings/about']) await go(page, p, 400)
  }
  await ctx.close()
}

// ---- phone ----
for (const dark of [false, true]) {
  const sfx = dark ? '-dark' : ''
  const { ctx, page } = await session({ phone: true, dark })
  await go(page, '/')
  await noOverflow(page, 'phone overview')
  await shot(page, 'phone-overview' + sfx)
  const id = await example(page)
  await go(page, `/parcels/${id}`)
  await noOverflow(page, 'phone parcel')
  await shot(page, 'phone-parcel' + sfx)
  if (!dark) {
    await go(page, '/parcels')
    await noOverflow(page, 'phone parcels')
    await shot(page, 'phone-parcels')
    await go(page, '/email')
    await noOverflow(page, 'phone email')
    await shot(page, 'phone-email')
    await go(page, '/settings/apps')
    await noOverflow(page, 'phone settings')
    await shot(page, 'phone-settings-apps')
    await go(page, '/')
    await addSheet(page, () => page.click('.fab'))
    await shot(page, 'phone-add')
  }
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error('browser errors:\n' + errors.join('\n'))
  process.exit(1)
}
console.log('ok')
