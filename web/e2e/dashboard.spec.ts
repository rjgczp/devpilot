import { expect, test } from '@playwright/test'

test('real Cookie/CSRF/PG workflow: register, login, create, authorize, pause, edit and reload', async ({ page }) => {
  const email = `dashboard-${Date.now()}@example.com`
  const password = 'local-dashboard-browser-test-phrase'
  const targetAttempts: string[] = []
  page.on('request', request => { if (/monitor-target\.example\.com/.test(request.url())) targetAttempts.push(request.url()) })
  await page.goto('/')
  await page.getByRole('button', { name: '创建账号', exact: true }).click()
  await page.getByLabel('邮箱', { exact: true }).fill(email)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '注册账号', exact: true }).click()
  await expect(page.getByText('账号已创建，请使用刚才的密码登录。')).toBeVisible()
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.getByRole('button', { name: '创建 Workspace', exact: true }).click()
  await page.getByRole('dialog').getByLabel('名称', { exact: true }).fill('Demo Workspace')
  await page.getByRole('dialog').getByRole('button', { name: '创建', exact: true }).click()
  await page.getByRole('button', { name: '创建 Project', exact: true }).click()
  await page.getByRole('dialog').getByLabel('名称', { exact: true }).fill('核心服务')
  await page.getByRole('dialog').getByRole('button', { name: '创建', exact: true }).click()
  await page.getByRole('button', { name: '创建监控', exact: true }).first().click()
  await page.getByLabel('监控名称', { exact: true }).fill('Payment API')
  await page.getByLabel('目标 URL', { exact: true }).fill('https://monitor-target.example.com/health')
  await page.getByRole('button', { name: '保存监控', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: 'Payment API' })
  await expect(row).toContainText('未授权')
  await row.getByRole('button', { name: '授权激活', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('https://monitor-target.example.com/health')
  await expect(page.getByRole('button', { name: '确认授权', exact: true })).toBeDisabled()
  await page.getByRole('checkbox').check()
  await page.getByRole('button', { name: '确认授权', exact: true }).click()
  await expect(row).toContainText('授权有效')
  await expect(row).toContainText('执行已阻断')
  await row.getByRole('button', { name: /Payment API.*HTTP GET/ }).click()
  await expect(page.getByRole('dialog')).toContainText('执行环境未开放')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await row.getByRole('button', { name: '查看 Payment API 历史', exact: true }).click()
  await expect(page.getByRole('heading', {name:'尚无检查历史'})).toBeVisible()
  await expect(page.getByText('执行环境未开放。历史记录不代表当前执行环境已开放。')).toBeVisible()
  await page.getByRole('button', {name:'← 返回监控'}).click()
  await row.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(row).toContainText('意图已暂停')
  await row.getByRole('button', { name: '编辑 Payment API' }).click()
  await page.getByLabel('监控名称', { exact: true }).fill('Payment API v2')
  await page.getByRole('button', { name: '保存监控', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: 'Payment API v2' })).toContainText('未授权')
  await page.reload()
  await expect(page.getByRole('row').filter({ hasText: 'Payment API v2' })).toBeVisible()
  const cookies = await page.context().cookies()
  expect(cookies.find(cookie => cookie.name === 'devpilot_session')?.httpOnly).toBe(true)
  expect(await page.evaluate(() => Object.keys(localStorage))).toEqual(['devpilot-theme'])
  expect(targetAttempts).toEqual([])
  await page.screenshot({ path: 'test-results/dashboard-light.png', fullPage: true })
  await page.getByRole('button', { name: '切换深色模式' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.screenshot({ path: 'test-results/dashboard-dark.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: 'test-results/dashboard-mobile.png', fullPage: true })
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
})

const monitor = { id: 'm1', workspace_id: 'w1', project_id: 'p1', name: '状态校验服务', version: '1', interval_seconds: 60, timeout_seconds: 10, expected_status: 200 }
const observation = { run_id: 'old-success', configuration_version: '1', activation_generation: '2', scheduler_epoch: '1', attempt_state: 'completed', http_status: 200, success: true, duration_ms: 32 }
const status = { workspace_id: 'w1', project_id: 'p1', monitor_id: 'm1', configuration_version: '1', activation_generation: '2', scheduler_epoch: '1', desired_state: 'enabled', authorization_state: 'valid', effective_authorization: true, scheduling_state: 'authorized_waiting', in_flight: true, current_observation: observation, latest_completed_observation: observation, latest_attempt: { ...observation, run_id: 'new-unknown', attempt_state: 'unknown', success: null as boolean | null, http_status: null as number | null } }

test('new unknown wins over old success; Members remain read-only; failed status read clears health', async ({ page }) => {
  let unavailable = false
  let current = status
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/execution-status')) return route.fulfill({ status: unavailable ? 503 : 200, json: unavailable ? { error: { code: 'service_unavailable' } } : { data: current } })
    let body
    if (path.endsWith('/auth/me')) body = { data: { id: 'member', email: 'member@example.com' } }
    else if (path.endsWith('/workspaces')) body = { data: [{ id: 'w1', name: 'Member workspace', role: 'member' }], pagination: { next_after: null } }
    else if (path.endsWith('/projects')) body = { data: [{ id: 'p1', workspace_id: 'w1', name: '服务' }], pagination: { next_after: null } }
    else body = { data: [monitor], pagination: { next_after: null } }
    await route.fulfill({ json: body })
  })
  await page.goto('/')
  const row = page.getByRole('row').filter({ hasText: '状态校验服务' })
  await expect(row).toContainText('结果未知')
  await expect(row).not.toContainText('已完成 · 正常')
  await expect(page.getByRole('button', { name: '创建监控', exact: true }).first()).toBeDisabled()
  for (const name of ['编辑 状态校验服务', '授权激活', '暂停']) await expect(row.getByRole('button', { name, exact: true })).toBeDisabled()
  current = { ...status, latest_attempt: { ...status.latest_attempt, attempt_state: 'started' } }
  await page.getByRole('button', { name: '刷新监控' }).click()
  await expect(row).toContainText('检查中')
  current = { ...status, in_flight: false, latest_attempt: { ...observation } }
  await page.getByRole('button', { name: '刷新监控' }).click()
  await expect(row).toContainText('已完成 · 正常')
  unavailable = true
  await page.getByRole('button', { name: '刷新监控' }).click()
  await expect(row).toContainText('状态不可用')
  await expect(row).not.toContainText('结果未知')
})

test('a stale edit preserves the captured ETag and is never silently replayed', async ({ page }) => {
  let writes = 0
  let tag = ''
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname
    if (request.method() === 'PATCH') { writes++; tag = request.headers()['if-match']; return route.fulfill({ status: 412, json: { error: { code: 'precondition_failed' } } }) }
    if (path.endsWith('/auth/me')) return route.fulfill({ json: { data: { id: 'owner', email: 'owner@example.com' } } })
    if (path.endsWith('/workspaces')) return route.fulfill({ json: { data: [{ id: 'w1', name: 'Owner workspace', role: 'owner' }], pagination: { next_after: null } } })
    if (path.endsWith('/projects')) return route.fulfill({ json: { data: [{ id: 'p1', name: '服务' }], pagination: { next_after: null } } })
    if (path.endsWith('/execution-status')) return route.fulfill({ json: { data: status } })
    if (path.endsWith('/m1')) return route.fulfill({ headers: { ETag: '"monitor:m1:1"' }, json: { data: { ...monitor, url: 'https://monitor-target.example.com/health' } } })
    return route.fulfill({ json: { data: [monitor], pagination: { next_after: null } } })
  })
  await page.goto('/')
  await page.getByRole('button', { name: '编辑 状态校验服务' }).click()
  await page.getByLabel('监控名称', { exact: true }).fill('用户尚未保存的编辑')
  await page.getByRole('button', { name: '保存监控', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('资源已被修改')
  await expect(page.getByLabel('监控名称', { exact: true })).toHaveValue('用户尚未保存的编辑')
  expect(writes).toBe(1); expect(tag).toBe('"monitor:m1:1"')
})

test('Workspace change cancels a late old-scope response and401 returns to login', async ({ page }) => {
  let lostSession = false
  let releaseOld: (() => void) | undefined
  const held = new Promise<void>(resolve => { releaseOld = resolve })
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (lostSession) return route.fulfill({ status: 401, json: { error: { code: 'unauthenticated' } } })
    if (path.endsWith('/auth/me')) return route.fulfill({ json: { data: { id: 'owner', email: 'owner@example.com' } } })
    if (path.endsWith('/workspaces')) return route.fulfill({ json: { data: [{ id: 'w1', name: 'Workspace A', role: 'owner' }, { id: 'w2', name: 'Workspace B', role: 'owner' }], pagination: { next_after: null } } })
    if (path.endsWith('/projects')) return route.fulfill({ json: { data: [{ id: path.includes('/w1/') ? 'p1' : 'p2', name: '默认 Project' }], pagination: { next_after: null } } })
    if (path.endsWith('/execution-status') && path.includes('/w1/')) { await held; return route.fulfill({ json: { data: status } }).catch(() => {}) }
    if (path.includes('/w2/')) return route.fulfill({ json: { data: [], pagination: { next_after: null } } })
    await route.fulfill({ json: { data: [monitor], pagination: { next_after: null } } })
  })
  await page.goto('/')
  await expect(page.getByRole('row').filter({ hasText: '状态校验服务' })).toBeVisible()
  await page.getByLabel('Workspace', { exact: true }).selectOption('w2')
  releaseOld?.()
  await expect(page.getByText('从第一个监控开始')).toBeVisible()
  await expect(page.getByText('状态校验服务')).toHaveCount(0)
  lostSession = true
  await page.getByRole('button', { name: '刷新监控' }).click()
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
})
