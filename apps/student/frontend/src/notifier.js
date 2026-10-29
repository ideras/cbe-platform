// The student's notifications: what is shown as a toast, for how long, and when a message may come back.
//
// Two ways in. post() is one occurrence (a failed login, a new correction of the volume): it is shown every time it is
// called. sync() is the set of conditions that hold right now (the server is unreachable, a save keeps failing): the
// window can pass it again on every poll, a condition is shown once when it starts, and once the student has dismissed it
// (or it has been on screen for its reading time) it stays quiet until it has ended and started again.

let nextId = 0;

export class Notifier {
  constructor({ maxVisible = 4, maxPending = 12, warningMs = 15000, onChange = () => {} } = {}) {
    this.maxVisible = maxVisible;
    this.maxPending = maxPending;
    this.warningMs = warningMs;
    this.onChange = onChange;
    this.items = [];
    this.managed = new Set();   // keys that sync() has put on screen and not yet seen end
    this.quiet = new Set();     // managed keys the student dismissed, or that were shown their time, while they still hold
  }

  // visible are the toasts on screen, oldest first. Blocking ones are chosen first, so a flood of ordinary warnings
  // waits behind them instead of pushing them off.
  visible() {
    const chosen = new Set([...this.items].sort((a, b) => Number(!a.sticky) - Number(!b.sticky)).slice(0, this.maxVisible));
    return this.items.filter(item => chosen.has(item));
  }
  get pending() { return this.items.length - this.visible().length; }

  // post shows one occurrence. The same key replaces the toast it already has, so a message never stacks on itself.
  post(key, text, { kind = 'warning', sticky = false } = {}) {
    if (!text) return;
    this.#put(key, text, kind, sticky);
    this.onChange();
  }

  // clear removes the toast of a key: its cause is over.
  clear(key) {
    if (this.#remove(key)) this.onChange();
  }

  // sync takes the conditions that hold: [{ key, text, kind, sticky }]. Those not held any more leave the screen at once.
  sync(conditions) {
    let changed = false;
    const held = new Set(conditions.map(condition => condition.key));
    for (const key of [...this.managed]) {
      if (held.has(key)) continue;
      this.managed.delete(key);
      this.quiet.delete(key);
      changed = this.#remove(key) || changed;
    }
    for (const { key, text, kind = 'warning', sticky = false } of conditions) {
      if (!text) continue;
      this.managed.add(key);
      if (this.quiet.has(key)) continue;
      changed = this.#put(key, text, kind, sticky, true) || changed;
    }
    if (changed) this.onChange();
  }

  // dismiss closes one toast. It changes nothing else: a condition that still holds is not shown again.
  dismiss(id) {
    const item = this.items.find(item => item.id === id);
    if (!item) return;
    this.items = this.items.filter(other => other !== item);
    if (this.managed.has(item.key)) this.quiet.add(item.key);
    this.onChange();
  }

  // tick lets time pass for the toasts on screen; the ones past their reading time go away.
  tick(ms) {
    let expired = false;
    for (const item of this.visible()) {
      if (item.sticky) continue;
      item.remaining -= ms;
      if (item.remaining > 0) continue;
      this.items = this.items.filter(other => other !== item);
      if (this.managed.has(item.key)) this.quiet.add(item.key);
      expired = true;
    }
    if (expired) this.onChange();
  }

  #remove(key) {
    const before = this.items.length;
    this.items = this.items.filter(item => item.key !== key);
    return this.items.length !== before;
  }

  #put(key, text, kind, sticky, managed = false) {
    const existing = this.items.find(item => item.key === key);
    if (existing) {
      // An occurrence starts the reading time over; a condition that still holds only updates its words.
      const changed = existing.text !== text || existing.kind !== kind || existing.sticky !== sticky;
      Object.assign(existing, { text, kind, sticky });
      if (!managed) existing.remaining = this.warningMs;
      return changed || !managed;
    }
    this.items.push({ id: ++nextId, key, text, kind, sticky, remaining: this.warningMs });
    // Bounded: the oldest ordinary toast that is not on screen is dropped first, then the oldest blocking one.
    while (this.items.length > this.maxVisible + this.maxPending) {
      const waiting = this.items.filter(item => !this.visible().includes(item));
      const drop = waiting.find(item => !item.sticky) ?? waiting[0];
      this.items = this.items.filter(item => item !== drop);
      if (this.managed.has(drop.key)) this.quiet.add(drop.key);
    }
    return true;
  }
}

// describeProblem is what a failed native call says, without the name of the error class in front of it.
export function describeProblem(problem) {
  const text = String(problem?.message ?? problem ?? '').replace(/^\s*(?:[A-Za-z]*Error:\s*)+/, '').trim();
  return text || 'Something went wrong. Try again.';
}

// volumeNotice turns the native outcome of a volume correction into the toast to show. The wording is chosen here but the
// outcome is only ever the application's: the window never works out a correction from the volume it is shown.
export function volumeNotice(notice) {
  if (notice.kind === 'failed') {
    return { text: 'The app could not restore the required volume. Keep your sound on and notify your instructor.', options: { kind: 'error', sticky: true } };
  }
  if (notice.initial) {
    return { text: `The volume was set to ${notice.minimum_percent}% so you can hear the exam alerts. It is put back when the exam ends.`, options: { kind: 'info' } };
  }
  return { text: `Please don't lower or mute the volume during the exam. The required volume has been restored to ${notice.minimum_percent}% so you can hear the alerts.`, options: { kind: 'warning' } };
}
