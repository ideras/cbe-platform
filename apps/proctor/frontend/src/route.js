// Every window of the dashboard loads the same page; the part of the address after # says which window it is. Go builds
// these addresses (internal/dashboard/windows.go); anything it does not recognize is the main window.
const sessionPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export function route(hash) {
  const [path, query = ''] = String(hash ?? '').replace(/^#/, '').split('?');
  if (path === '/monitoring') return { page: 'monitoring', props: {} };
  const review = path.match(/^\/review\/(.+)$/);
  if (review && sessionPattern.test(review[1])) return { page: 'review', props: { sessionId: review[1] } };
  if (path === '/image') {
    const params = new URLSearchParams(query);
    const ids = (params.get('set') ?? '').split(',').filter(Boolean).map(Number);
    const times = (params.get('times') ?? '').split(',').filter(Boolean);
    const start = Number(params.get('at') ?? 0);
    if (ids.length && ids.every(id => Number.isSafeInteger(id) && id > 0) && Number.isSafeInteger(start) && start >= 0 && start < ids.length) return { page: 'image', props: { ids, times, start } };
  }
  return { page: 'main', props: {} };
}
