import * as api from '$proctor-api';
// Shows the monitoring window of an exam, first making it the monitored one. It never changes what is published: that is
// only done by publishing. Shared by the exams of a class and by the "Now admitting" bar.
export async function monitorExam(view, examId, onSelected) {
  if (examId !== view.exam_id) { await api.SelectExam(examId); await onSelected(); }
  await api.OpenMonitoring();
}
