import { test, expect } from '@playwright/test';

const sourceIds = [
  'local-duckdb', 'example-duckdb-parquet',
  'local-ducklake', 'example-ducklake-tls',
];

test.beforeEach(async ({ page }) => {
  const response = await page.request.post('/login', {
    data: { user: 'admin', password: process.env.GRAFANA_ADMIN_PASSWORD ?? 'admin' },
  });
  expect(response.ok()).toBeTruthy();
});

test('all four provisioned sources have sample data and keep ingesting', async ({ page }) => {
  const configured = await page.request.get('/api/datasources');
  const sources = await configured.json();
  expect(sources.map((source: { uid: string }) => source.uid).sort()).toEqual([...sourceIds].sort());
  const query = async (uid: string, sql: string) => {
    const response = await page.request.post('/api/ds/query', { data: {
      from: 'now-30m', to: 'now', queries: [{ refId: 'A', datasource: { uid, type: 'local-duckdb-datasource' }, rawSql: sql, format: 'table' }],
    } });
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.results.A.error).toBeUndefined();
    return body.results.A.frames[0].data.values;
  };
  for (const uid of sourceIds) {
    const health = await page.request.get(`/api/datasources/uid/${uid}/health`);
    expect(health.ok()).toBeTruthy();
    expect((await health.json()).status).toBe('OK');
    const table = uid === 'local-duckdb' || uid === 'example-duckdb-parquet' ? 'example_metrics' : 'measurements';
    const values = await query(uid, `SELECT count(*) AS samples, count(DISTINCT device) AS devices FROM ${table} WHERE $__timeFilter(time)`);
    expect(values[0][0]).toBeGreaterThan(0);
    expect(values[1][0]).toBe(3);
  }
  const tlsSource = sources.find((source: { uid: string }) => source.uid === 'example-ducklake-tls');
  expect(tlsSource.jsonData.postgresSslMode).toBe('verify-full');
  expect(tlsSource.jsonData.postgresUser).toBe('grafana_reader');
  const tls = await query('example-ducklake-tls', "SELECT ssl, client_dn FROM postgres_query('__ducklake_metadata_lake', 'SELECT ssl, client_dn FROM pg_stat_ssl WHERE pid = pg_backend_pid()')");
  expect(tls[0][0]).toBe(true);
  expect(tls[1][0]).toContain('CN=grafana_reader');
  for (const uid of sourceIds) {
    const table = uid === 'local-duckdb' || uid === 'example-duckdb-parquet' ? 'example_metrics' : 'measurements';
    const initial = (await query(uid, `SELECT epoch(max(time)) AS newest FROM ${table}`))[0][0];
    await expect.poll(async () => (await query(uid, `SELECT epoch(max(time)) AS newest FROM ${table}`))[0][0], { timeout: 20000 }).toBeGreaterThan(initial);
  }
});

test('the example dashboard renders every source and applies its device variable', async ({ page }, testInfo) => {
  const seen = new Set<string>();
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', async response => {
    if (!response.url().includes('/api/ds/query') || response.request().method() !== 'POST') return;
    try {
      const request = response.request().postDataJSON();
      const body = await response.json();
      for (const target of request.queries) {
        const uid = target.datasource?.uid;
        if (!sourceIds.includes(uid) || target.format !== 'time_series') continue;
        const result = body.results[target.refId];
        if (!response.ok() || result?.error) { errors.push(result?.error ?? `HTTP ${response.status()}`); continue; }
        const frame = result?.frames?.[0];
        if (!frame?.data?.values?.[0]?.length) continue;
        const labels = frame.schema.fields.flatMap((field: { labels?: { device?: string } }) => field.labels?.device ? [field.labels.device] : []);
        if (!labels.length || labels.some((device: string) => device !== 'sensor-2')) { errors.push(`Unexpected device labels for ${uid}: ${labels}`); continue; }
        seen.add(uid);
      }
    } catch (error) { errors.push(String(error)); }
  });
  await page.goto('/d/duckdb-ducklake-examples?var-device=sensor-2');
  await expect(page.getByText('DuckDB / DuckLake examples', { exact: true }).first()).toBeVisible();
  for (const title of ['Embedded DuckDB · inline SQL', 'Embedded DuckDB · Parquet', 'DuckLake · local catalog', 'DuckLake · TLS catalog']) {
    await page.getByRole('heading', { name: title, exact: true }).scrollIntoViewIfNeeded();
  }
  await expect.poll(() => seen.size).toBe(4);
  await expect(page.getByRole('columnheader', { name: 'temperature', exact: true })).toBeVisible();
  expect(errors).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath('example-dashboard.png'), fullPage: true });
});
