const ranks = { running: 0, queued: 1 };

export function gradeStatus(workspace) {
  if (!workspace?.submissions) return 'none';
  if (!workspace.grade) return 'ungraded';
  if (workspace.grade.state === 'done') return workspace.grade.confirmed ? 'confirmed' : 'advisory';
  return workspace.grade.state;
}

export function gradingRows(sessions, missing, workspaces) {
  const rows = [...sessions.map(student => {
    const workspace = workspaces.get(student.session_id);
    return { student, workspace, status: gradeStatus(workspace) };
  }), ...missing.map(student => ({ student, status: 'none' }))];
  return rows.sort((a, b) => (ranks[a.status] ?? 2) - (ranks[b.status] ?? 2)
    || a.student.student_name.localeCompare(b.student.student_name)
    || a.student.account_number.localeCompare(b.student.account_number));
}

// A roster summary can show a different run (notably a confirmed grade). Only matching IDs count.
// The caller keeps observed terminal results, but must not carry forward running/queued states.
export function batchProgress(ids, current, finished) {
  const counts = { done: 0, failed: 0, running: 0, queued: 0, unseen: 0 };
  for (const id of ids) {
    const state = finished.get(id) ?? current.get(id);
    counts[Object.hasOwn(counts, state) ? state : 'unseen']++;
  }
  return { ...counts, total: ids.length, complete: ids.length === counts.done + counts.failed };
}
