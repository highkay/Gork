export function normalizeTaskProgress(task = {}) {
  const total = Math.max(0, Number(task.total || 0));
  const completed = Math.min(total, Math.max(0, Number(task.completed || 0)));
  return {
    id: task.id || task.task_id || '',
    status: task.status || 'pending',
    completed,
    total,
    percent: total > 0 ? Math.round((completed / total) * 100) : 0,
    requestId: task.request_id || task.requestId || '',
  };
}
