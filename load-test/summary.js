// Общий модуль форматирования результатов k6.
// Импортируется всеми сценариями.
//
// Запускай тесты из КОРНЯ проекта:
//   k6 run load-test/scenario_a.js
// Тогда файлы results_a.json / results_a.txt создадутся в корне.

/**
 * Форматирует итоговый отчёт в читаемый текст.
 * @param {object} data  — объект из handleSummary(data)
 * @param {string} title — заголовок отчёта
 */
export function formatSummary(data, title) {
  const m = data.metrics;
  const lines = [];
  const sep = '─'.repeat(60);

  lines.push(sep);
  lines.push(` ${title}`);
  lines.push(` Дата: ${new Date().toISOString()}`);
  lines.push(sep);

  // ── Trend-метрика (latency, длительность запросов)
  function trend(key, label) {
    const v = m[key];
    if (!v || v.type !== 'trend') return;
    const vals = v.values;
    const thr  = v.thresholds ? thresholdStatus(v.thresholds) : '';
    lines.push(`\n ${label} ${thr}`);
    lines.push(`   avg=${fmt(vals.avg)}  min=${fmt(vals.min)}  med=${fmt(vals.med)}  max=${fmt(vals.max)}`);
    lines.push(`   p90=${fmt(vals['p(90)'])}  p95=${fmt(vals['p(95)'])}  p99=${fmt(vals['p(99)'])}`);
  }

  // ── Counter-метрика (количество)
  function counter(key, label) {
    const v = m[key];
    if (!v || v.type !== 'counter') return;
    lines.push(` ${label}: ${v.values.count}`);
  }

  // ── Rate-метрика (процент)
  function rate(key, label) {
    const v = m[key];
    if (!v || v.type !== 'rate') return;
    const thr = v.thresholds ? thresholdStatus(v.thresholds) : '';
    lines.push(` ${label}: ${(v.values.rate * 100).toFixed(2)}% ${thr}`);
  }

  lines.push('\n── Кастомные метрики ──────────────────────────────────');
  trend('message_latency_ms',       'Latency сообщений (ms)');
  trend('auth_latency_ms',          'Latency auth-service (ms)');
  trend('rooms_latency_ms',         'Latency rooms-service (ms)');
  trend('post_message_latency_ms',  'Latency POST /messages (ms)');
  lines.push('');
  counter('messages_sent',     'Отправлено сообщений');
  counter('messages_received', 'Получено сообщений');

  lines.push('\n── HTTP ───────────────────────────────────────────────');
  trend('http_req_duration', 'HTTP длительность (ms)');
  lines.push('');
  rate('http_req_failed',    'HTTP ошибки');
  counter('http_reqs',       'HTTP запросов всего');

  lines.push('\n── VU / Итерации ──────────────────────────────────────');
  if (m.iterations)  counter('iterations',  'Итерации завершены');
  if (m.vus_max) {
    lines.push(` VU максимум: ${m.vus_max.values.max}`);
  }
  if (m.iteration_duration) trend('iteration_duration', 'Длительность итерации (ms)');

  lines.push('\n── Пороги (thresholds) ────────────────────────────────');
  for (const [name, metric] of Object.entries(m)) {
    if (!metric.thresholds) continue;
    for (const [expr, result] of Object.entries(metric.thresholds)) {
      const status = result.ok ? '✓ OK  ' : '✗ FAIL';
      lines.push(` [${status}]  ${name}: ${expr}`);
    }
  }

  lines.push('\n' + sep + '\n');
  return lines.join('\n');
}

function fmt(n) {
  if (n === undefined || n === null) return 'n/a';
  return n.toFixed(2) + 'ms';
}

function thresholdStatus(thresholds) {
  const allOk = Object.values(thresholds).every(t => t.ok);
  return allOk ? '✓' : '✗ threshold failed';
}
