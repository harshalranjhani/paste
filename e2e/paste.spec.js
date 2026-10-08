const { test, expect } = require('@playwright/test');

const password = 'correct-horse-battery-staple';

test.beforeAll(async ({ request }) => {
  const setup = await request.post('/setup', { form: { username: 'admin', password }, maxRedirects: 0 });
  expect(setup.status()).toBe(303);
});

async function login(page) {
  await page.goto('/login');
  await page.getByLabel('Username').fill('admin');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.waitForURL('/');
}

async function addFiles(page, files) {
  await page.goto('/new');
  for (let i = 0; i < files.length; i++) {
    if (i) await page.getByRole('button', { name: 'Add file' }).click();
    await page.getByLabel(`File path ${i + 1}`, { exact: true }).fill(files[i].path);
    await page.getByLabel(`File contents ${i + 1}`, { exact: true }).fill(files[i].content);
  }
}

test('slug choices create usable links and duplicate custom names preserve the original paste', async ({ page, context }, testInfo) => {
  await login(page);
  for (const choice of ['short', 'long', 'custom']) {
    await addFiles(page, [{ path: 'README.md', content: '# Slug preview\n\nOriginal contents.' }]);
    const selector = page.getByRole('combobox', { name: 'Random slug', exact: true });
    await expect(selector).toHaveValue('long');
    await selector.selectOption(choice === 'custom' ? 'short' : choice);
    if (choice === 'custom') await page.getByLabel('Custom slug', { exact: false }).fill('browser-notes_2026');
    await page.getByRole('button', { name: 'Create paste' }).click();
    const id = new URL(page.url()).pathname.split('/').pop();
    if (choice === 'custom') expect(id).toBe('browser-notes_2026');
    else expect(id).toMatch(choice === 'short' ? /^[A-Za-z0-9]{8}$/ : /^[A-Za-z0-9]{9,22}$/);
    await page.getByRole('link', { name: 'Preview', exact: true }).click();
    await expect(page.frameLocator('iframe').getByRole('heading', { name: 'Slug preview' })).toBeVisible();
    // Native form submissions use CRLF for textarea line endings.
    expect(await (await context.request.get(`/p/${id}/raw`)).text()).toBe('# Slug preview\r\n\r\nOriginal contents.');
    const download = page.waitForEvent('download');
    await page.getByRole('link', { name: 'Download ZIP' }).click();
    expect((await download).suggestedFilename()).toBe(`${id}.zip`);
  }
  await addFiles(page, [{ path: 'replacement.txt', content: 'Replacement contents.' }]);
  await page.getByLabel('Custom slug', { exact: false }).fill('../unsafe');
  await page.getByRole('button', { name: 'Create paste' }).click();
  await expect(page).toHaveURL('/new');
  expect(await page.getByLabel('Custom slug', { exact: false }).evaluate(input => input.validity.patternMismatch)).toBe(true);
  await page.getByLabel('Custom slug', { exact: false }).fill('browser-notes_2026');
  await page.screenshot({ path: testInfo.outputPath('slug-sharing-settings.png'), fullPage: true });
  await page.getByRole('button', { name: 'Create paste' }).click();
  await expect(page.getByRole('alert')).toContainText('already in use');
  expect(await (await context.request.get('/p/browser-notes_2026/raw')).text()).toBe('# Slug preview\r\n\r\nOriginal contents.');
});

test('previews preserve file navigation, history, themes, mobile selection and source access', async ({ page, context }, testInfo) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  let externalRequests = 0;
  await page.route('https://example.com/**', route => { externalRequests++; return route.fulfill({ body: 'blocked' }); });
  await login(page);
  await addFiles(page, [
    { path: 'README.md', content: '# Preview notes\n\n**Ready** to share.\n\n| File | State |\n| --- | --- |\n| app | done |' },
    { path: 'index.html', content: '<h1>HTML preview</h1><script>window.previewScriptRan=true;parent.document.body.dataset.hacked="yes"</script><img src="https://example.com/tracker"><form action="/logout" method="post"><button>Submit unsafe form</button></form>' },
    { path: 'notes.txt', content: 'plain-file-source' },
  ]);
  await page.getByRole('combobox', { name: 'Random slug', exact: true }).selectOption('short');
  await page.getByRole('button', { name: 'Create paste' }).click();
  expect(new URL(page.url()).pathname.split('/').pop()).toMatch(/^[A-Za-z0-9]{8}$/);
  await expect(page.locator('#selected-path')).toHaveText('README.md');
  await page.getByRole('link', { name: 'Preview', exact: true }).click();
  const frame = page.frameLocator('iframe.file-preview');
  await expect(frame.getByRole('heading', { name: 'Preview notes' })).toBeVisible();
  await expect(frame.locator('table')).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('markdown-desktop.png'), fullPage: true });
  await page.getByRole('button', { name: 'Switch to light mode' }).click();
  await expect(frame.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.getByRole('treeitem', { name: 'index.html', exact: true }).click();
  await expect(frame.getByRole('heading', { name: 'HTML preview' })).toBeVisible();
  expect(await page.locator('iframe').getAttribute('sandbox')).toBe('');
  expect(await frame.locator('body').evaluate(() => window.previewScriptRan)).toBeUndefined();
  expect(await page.locator('body').getAttribute('data-hacked')).toBeNull();
  await frame.getByRole('button', { name: 'Submit unsafe form' }).click();
  await expect(frame.getByRole('heading', { name: 'HTML preview' })).toBeVisible();
  expect(externalRequests).toBe(0);
  await page.getByRole('treeitem', { name: 'notes.txt', exact: true }).click();
  await expect(page.locator('#code-pane')).toContainText('plain-file-source');
  await expect(page.getByRole('link', { name: 'Preview', exact: true })).toBeHidden();
  await page.goBack();
  await expect(frame.getByRole('heading', { name: 'HTML preview' })).toBeVisible();
  await page.goBack();
  await expect(frame.getByRole('heading', { name: 'Preview notes' })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByLabel('Select file').selectOption({ label: 'index.html' });
  await expect(frame.getByRole('heading', { name: 'HTML preview' })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('html-mobile.png'), fullPage: true });
  await page.getByRole('link', { name: 'Code', exact: true }).click();
  await expect(page.locator('#code-pane')).toContainText('window.previewScriptRan');
  const rawURL = await page.locator('#raw-link').getAttribute('href');
  const raw = await context.request.get(rawURL);
  expect(await raw.text()).toContain('<h1>HTML preview</h1>');
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.getByRole('button', { name: 'Copy contents', exact: true }).click();
  await expect(page.getByRole('status')).toHaveText('File contents copied.');
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('<h1>HTML preview</h1>');
  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('link', { name: 'Download ZIP' }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/\.zip$/);
  expect(await download.failure()).toBeNull();
  expect(errors).toEqual([]);
});

test('password burn sharing requires reveal and only the winning browser can browse and download', async ({ page, browser }, testInfo) => {
  await login(page);
  await addFiles(page, [
    { path: 'README.md', content: '# Private notes\n\nOne reader only.' },
    { path: 'secret.html', content: '<h1>Private HTML</h1>' },
  ]);
  await page.getByLabel('Burn after read', { exact: true }).check();
  await page.getByLabel('Custom slug', { exact: false }).fill('burn-with-custom-slug');
  await page.getByLabel('Password', { exact: false }).fill('burn-password');
  await page.getByRole('button', { name: 'Create paste' }).click();
  const pasteURL = page.url();
  const id = new URL(pasteURL).pathname.split('/').pop();
  expect(id).toBe('burn-with-custom-slug');
  const firstContext = await browser.newContext({ baseURL: new URL(pasteURL).origin });
  const secondContext = await browser.newContext({ baseURL: new URL(pasteURL).origin });
  try {
    const first = await firstContext.newPage();
    const second = await secondContext.newPage();
    await first.goto(pasteURL);
    await expect(first.getByRole('heading', { name: 'This paste is password protected.' })).toBeVisible();
    await expect(first.locator('body')).not.toContainText('README.md');
    expect((await firstContext.request.get(`/p/${id}/raw`)).status()).toBe(401);
    await first.getByLabel('Password', { exact: true }).fill('wrong');
    await first.getByRole('button', { name: 'Unlock paste' }).click();
    await expect(first.getByRole('alert')).toContainText('Invalid password.');
    for (const reader of [first, second]) {
      await reader.goto(pasteURL);
      await reader.getByLabel('Password', { exact: true }).fill('burn-password');
      await reader.getByRole('button', { name: 'Unlock paste' }).click();
      await expect(reader.getByRole('button', { name: 'Reveal paste' })).toBeVisible();
      await reader.reload();
      await expect(reader.getByRole('button', { name: 'Reveal paste' })).toBeVisible();
      expect((await reader.context().request.get(`/p/${id}/archive.zip`)).status()).toBe(403);
    }
    await first.screenshot({ path: testInfo.outputPath('burn-reveal.png'), fullPage: true });
    await Promise.all([first, second].map(reader => reader.getByRole('button', { name: 'Reveal paste' }).click()));
    const firstWon = await first.locator('#code-pane').count() === 1;
    const winner = firstWon ? first : second;
    const loser = firstWon ? second : first;
    await expect(winner.locator('#selected-path')).toHaveText('README.md');
    await expect(loser.getByRole('heading', { name: 'Let’s try that again.' })).toBeVisible();
    await expect(loser.locator('body')).toContainText('already been revealed');
    expect((await loser.context().request.get(`/p/${id}/raw`)).status()).toBe(410);
    await winner.reload();
    await winner.getByRole('link', { name: 'Preview', exact: true }).click();
    await expect(winner.frameLocator('iframe').getByRole('heading', { name: 'Private notes' })).toBeVisible();
    await winner.getByRole('treeitem', { name: 'secret.html', exact: true }).click();
    await expect(winner.frameLocator('iframe').getByRole('heading', { name: 'Private HTML' })).toBeVisible();
    const rawURL = await winner.locator('#raw-link').getAttribute('href');
    expect(await (await winner.context().request.get(rawURL)).text()).toBe('<h1>Private HTML</h1>');
    expect((await loser.context().request.get(rawURL)).status()).toBe(410);
    const downloadPromise = winner.waitForEvent('download');
    await winner.getByRole('link', { name: 'Download ZIP' }).click();
    expect(await (await downloadPromise).failure()).toBeNull();
    expect((await loser.context().request.get(`/api/v1/pastes/${id}/archive.zip`)).status()).toBe(410);
  } finally {
    await firstContext.close();
    await secondContext.close();
  }
});
