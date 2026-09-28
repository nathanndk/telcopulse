// Only the configured public viewer URL is sent to the browser, never exporter credentials.
export function traceViewerBase(value: string | undefined): string | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) return null;
    return url.href.replace(/\/$/, '');
  } catch { return null; }
}

export function traceLinks(base: string | null, traceId: string, transactionId: string) {
  if (!base) return null;
  const search = new URLSearchParams({
    service: 'api-gateway', operation: 'purchase.resume',
    tags: JSON.stringify({ 'transaction.id': transactionId }),
    lookback: '2d', limit: '100',
  });
  return {
    original: /^[a-f0-9]{32}$/.test(traceId) ? `${base}/trace/${traceId}` : null,
    attempts: `${base}/search?${search}`,
  };
}
