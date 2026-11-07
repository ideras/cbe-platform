<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import Card from '../../../../shared/theme/Card.svelte';
  import { spokenSpan } from './spans.js';

  // The lock of the exam, in place of the questions. lock.reason is "away", "no_contact", "network" (the student asked for
  // it, to reconnect) or empty while the server has not said why yet; policy holds the lock times the exam was published with.
  let { lock, policy = {}, status = null, timingStale = true } = $props();

  let reason = $derived.by(() => {
    if (lock?.reason === 'away') {
      return policy.lock_away_after_ms ? `You were away from the exam window for ${spokenSpan(policy.lock_away_after_ms)}.` : 'You were away from the exam window for too long.';
    }
    if (lock?.reason === 'no_contact') {
      return policy.lock_no_contact_after_ms ? `The app could not reach the server for ${spokenSpan(policy.lock_no_contact_after_ms)}.` : 'The app could not reach the server for too long.';
    }
    if (lock?.reason === 'network') return 'You locked the exam to change network.';
    return '';
  });
</script>

<Card label="Exam locked" title="Your exam is locked" icon="lock" level={2} tone="warning">
  {#if reason}<p>{reason}</p>{/if}
  <p><strong>Call your instructor. The exam time keeps running.</strong></p>
  {#if lock?.reason === 'network' || lock?.reason === 'no_contact'}
    <p>This window is not full screen now, so you can reach your network settings. Screenshots of all your screens are taken while the exam is locked. When you are connected to the exam network again and back in this window, tell your instructor: only they can unlock the exam, and the window goes back to full screen then.</p>
  {/if}
  <p>{status?.status === 'grace' ? 'Grace time remaining' : 'Remaining time'} (last server report): <strong class="time">{remainingTime(status)}</strong>.</p>
  {#if timingStale}<p role="alert">Timing stale / disconnected — waiting for a current server report.</p>{/if}
  <p>No questions are shown while the exam is locked. Your unsaved answers are kept and sent once your instructor unlocks the exam.</p>
</Card>

<style>
  /* The remaining time is a monospace chip on the warm fill, as everywhere else. */
  .time { display: inline-block; padding: 0 .45rem; font-family: var(--font-mono); color: var(--warn-text); background: var(--warn-fill); border: 1px solid var(--warn-border); border-radius: 5px; }
</style>
