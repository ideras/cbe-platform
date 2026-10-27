export class AnswerQueue {
  constructor(update, onError = () => {}) {
    this.update = update;
    this.onError = onError;
    this.pending = null;
    this.running = null;
    this.error = null;
    this.closed = false;
  }

  stage(answers, revision) {
    if (this.closed) return;
    this.pending = { answers: { ...answers }, revision };
    this.start();
  }

  start() {
    if (this.running || !this.pending || this.closed) return;
    this.running = this.drain().finally(() => { this.running = null; this.start(); });
  }

  async drain() {
    while (this.pending && !this.closed) {
      const work = this.pending;
      this.pending = null;
      try {
        await this.update(work.answers, work.revision);
        this.error = null;
      } catch (error) {
        this.error = error;
        this.onError(error);
      }
    }
  }

  async flush() {
    while (this.running || this.pending) {
      this.start();
      await this.running;
    }
    if (this.error) throw this.error;
  }

  close() { this.closed = true; this.pending = null; }
}

export function saveLabel(view, revision) {
  if (view.save_error) return `Save failed — text remains local. ${view.save_error}`;
  if (view.save_state === 'saving') return 'Saving… waiting for server acknowledgment.';
  if (revision > view.acknowledged_revision) return 'Unsaved changes — retained only while this application remains open.';
  if (view.saved) return `Saved — server version ${view.saved.version_seq}, ${view.saved.saved_at}.`;
  return 'No saved version yet.';
}
