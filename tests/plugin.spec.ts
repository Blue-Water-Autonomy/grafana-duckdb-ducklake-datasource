import { test, expect, type Locator } from '@playwright/test';

async function replaceSQL(editor: Locator, sql: string) {
  await editor.click();
  await editor.press('ControlOrMeta+A');
  await editor.press('Backspace');
  await editor.fill(sql);
}

test.beforeEach(async ({ page }) => {
  const login = await page.request.post('/login', { data: { user: 'admin', password: process.env.GRAFANA_ADMIN_PASSWORD ?? 'admin' } });
  expect(login.ok()).toBeTruthy();
});

test('embedded configuration and real SQL results in Explore', async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/connections/datasources/edit/local-duckdb');
  await expect(page.getByRole('heading', { name: 'Local DuckDB', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Database file', exact: true })).toHaveValue('/var/lib/grafana/duckdb/analytics.duckdb');
  await page.getByRole('button', { name: 'Test', exact: true }).click();
  await expect(page.getByText('Embedded DuckDB is ready; setup SQL completed.', { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('embedded-settings.png'), fullPage: true });
  await page.getByRole('link', { name: 'Explore data', exact: true }).filter({ hasText: 'Explore data' }).click();
  const editor = page.getByRole('textbox', { name: 'Editor content;Press Alt+F1 for Accessibility Options.', exact: true });
  await expect(editor).toBeVisible();
  await replaceSQL(editor, 'SELECT 4242 AS verified_value');
  await page.getByRole('button', { name: 'Run query', exact: true }).filter({ hasText: 'Run query' }).click();
  await expect(page.getByRole('columnheader', { name: 'verified_value', exact: true })).toBeVisible();
  await expect(page.getByRole('gridcell', { name: '4242', exact: true })).toBeVisible();
  await replaceSQL(editor, 'SELECT time, device, value FROM example_metrics WHERE $__timeFilter(time) ORDER BY time');
  await page.getByRole('combobox', { name: 'Query format', exact: true }).click();
  await page.getByText('Time series', { exact: true }).click();
  const responsePromise = page.waitForResponse(response => response.url().includes('/api/ds/query') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Run query', exact: true }).filter({ hasText: 'Run query' }).click();
  const response = await responsePromise;
  expect(response.ok()).toBeTruthy();
  const body = await response.json();
  expect(body.results.A.frames[0].schema.meta.type).toBe('timeseries-wide');
  await expect(page.getByRole('heading', { name: 'Graph', exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('time-series.png'), fullPage: true });
  expect(errors).toEqual([]);
});

test('DuckLake connects through PostgreSQL and returns ingested rows', async ({ page }, testInfo) => {
  await page.goto('/connections/datasources/edit/local-ducklake');
  await expect(page.getByRole('textbox', { name: 'PostgreSQL host', exact: true })).toHaveValue('postgres');
  await page.getByRole('button', { name: 'Test', exact: true }).click();
  await expect(page.getByText('DuckLake is ready (PostgreSQL catalog, read-only queries).', { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('ducklake-settings.png'), fullPage: true });
  const response = await page.request.post('/api/ds/query', { data: {
    from: 'now-1h', to: 'now', queries: [{ refId: 'A', datasource: { uid: 'local-ducklake', type: 'local-duckdb-datasource' },
      rawSql: 'SELECT count(*) AS ingested_rows FROM measurements', format: 'table', intervalMs: 1000, maxDataPoints: 100 }],
  } });
  expect(response.ok()).toBeTruthy();
  const body = await response.json();
  expect(body.results.A.frames[0].data.values[0][0]).toBeGreaterThan(0);
});

test('an administrator can configure embedded setup and choose each mode', async ({ page }, testInfo) => {
  const uid = `ui-test-${Date.now()}`;
  let lastSaveDate: string | undefined;
  const saveAndTest = async () => {
    // Grafana's SDK invalidates instances by the settings update timestamp.
    // Wait for the server clock to advance between rapid automated saves.
    if (lastSaveDate) {
      await expect.poll(async () => (await page.request.get('/api/health')).headers().date).not.toBe(lastSaveDate);
    }
    const saved = page.waitForResponse(response => response.url().endsWith(`/api/datasources/uid/${uid}`) && response.request().method() === 'PUT');
    await page.getByRole('button', { name: 'Save & test', exact: true }).click();
    const response = await saved;
    expect(response.ok()).toBeTruthy();
    lastSaveDate = response.headers().date;
  };
  const created = await page.request.post('/api/datasources', { data: {
    uid, name: uid, type: 'local-duckdb-datasource', access: 'proxy',
    jsonData: { path: `/tmp/${uid}.duckdb` },
  } });
  expect(created.ok()).toBeTruthy();
  try {
    await page.goto(`/connections/datasources/edit/${uid}`);
    await expect(page.getByRole('textbox', { name: 'Database file', exact: true })).toBeEnabled();
    await expect(page.getByRole('textbox', { name: 'Setup SQL file', exact: true })).toBeVisible();
    await page.getByRole('combobox', { name: 'Database mode', exact: true }).click();
    await expect(page.getByRole('option')).toHaveCount(2);
    await page.getByRole('option', { name: 'Embedded DuckDB — file with setup SQL', exact: true }).click();
    const editor = page.getByRole('textbox', { name: 'Editor content;Press Alt+F1 for Accessibility Options.', exact: true });
    await replaceSQL(editor, 'CREATE OR REPLACE VIEW ui_example AS SELECT 42 AS value;');
    await saveAndTest();
    await expect(page.getByText('Embedded DuckDB is ready; setup SQL completed.', { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath('editable-embedded-settings.png'), fullPage: true });
    const response = await page.request.post('/api/ds/query', { data: { from: 'now-1h', to: 'now', queries: [
      { refId: 'A', datasource: { uid, type: 'local-duckdb-datasource' }, rawSql: 'SELECT * FROM ui_example', format: 'table' },
    ] } });
    expect(response.ok()).toBeTruthy();
    expect((await response.json()).results.A.frames[0].data.values[0][0]).toBe(42);
    await page.getByRole('combobox', { name: 'Database mode', exact: true }).click();
    await page.getByText('DuckLake — PostgreSQL catalog', { exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'PostgreSQL host', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'DuckLake data directory', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Database file', exact: true })).toHaveCount(0);
    await expect(page.getByText('Verify certificate and hostname (verify-full)', { exact: true })).toBeVisible();
    await page.getByRole('textbox', { name: 'PostgreSQL host', exact: true }).fill('catalog.example.com');
    await page.getByRole('textbox', { name: 'Catalog database', exact: true }).fill('ducklake');
    await page.getByRole('textbox', { name: 'PostgreSQL user', exact: true }).fill('reader');
    await page.getByRole('textbox', { name: 'DuckLake data directory', exact: true }).fill('/var/lib/ducklake/local');
    await saveAndTest();
    await expect(page.getByText('PostgreSQL TLS mode verify-full requires a CA certificate file', { exact: false })).toBeVisible();
    await page.getByRole('textbox', { name: 'CA certificate file', exact: true }).fill('/certs/ca.crt');
    await page.getByRole('textbox', { name: 'Client certificate file', exact: true }).fill('/certs/client.crt');
    await page.getByRole('textbox', { name: 'Client private key file', exact: true }).fill('/certs/client.key');
    await page.screenshot({ path: testInfo.outputPath('remote-catalog-tls.png'), fullPage: true });
    await page.getByRole('combobox', { name: 'PostgreSQL TLS mode', exact: true }).click();
    await page.getByText('Disabled (disable)', { exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'CA certificate file', exact: true })).toHaveCount(0);
    await page.getByRole('textbox', { name: 'PostgreSQL host', exact: true }).fill('postgres');
    await page.getByRole('textbox', { name: 'PostgreSQL user', exact: true }).fill('ducklake');
    await page.getByLabel('PostgreSQL password', { exact: true }).fill(process.env.DUCKLAKE_PASSWORD ?? 'local-development');
    await page.getByRole('textbox', { name: 'Extension directory', exact: true }).fill('/opt/duckdb/extensions');
    await saveAndTest();
    await expect(page.getByText('DuckLake is ready (PostgreSQL catalog, read-only queries).', { exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByText('Disabled (disable)', { exact: true })).toBeVisible();
    const saved = await page.request.get(`/api/datasources/uid/${uid}`);
    const dataSource = await saved.json();
    expect(dataSource.jsonData.postgresSslMode).toBe('disable');
    expect(dataSource.jsonData.postgresSslRootCert).toBeFalsy();
    expect(dataSource.jsonData.postgresSslCert).toBeFalsy();
    expect(dataSource.jsonData.postgresSslKey).toBeFalsy();
    expect(dataSource.jsonData.setupSql).toBeFalsy();
  } finally {
    await page.request.delete(`/api/datasources/uid/${uid}`);
  }
});
