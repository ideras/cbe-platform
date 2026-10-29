<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import Card from '../../../../shared/theme/Card.svelte';

  // The lock of the exam, in place of the questions. lock.reason is "away", "no_contact" or empty while the server has not
  // said why yet; policy holds the lock times the exam was published with.
  let { lock, policy = {}, status = null, timingStale = true } = $props();

  // A lock time in the words the screen uses.
  function minutes(millis) {
    const count = Math.max(1, Math.round(millis / 60000));
    return count === 1 ? '1 minute' : `${count} minutes`;
  }
  let reason = $derived.by(() => {
    if (lock?.reason === 'away') {
      return policy.lock_away_after_ms ? `You were away from the exam window for ${minutes(policy.lock_away_after_ms)}.` : 'You were away from the exam window for too long.';
    }
    if (lock?.reason === 'no_contact') {
      return policy.lock_no_contact_after_ms ? `The app could not reach the server for ${minutes(policy.lock_no_contact_after_ms)}.` : 'The app could not reach the server for too long.';
    }
    return '';
  });
</script>

<Card label="Exam locked" title="Your exam is locked" level={2} tone="warning">
  {#if reason}<p>{reason}</p>{/if}
  <p><strong>Call your instructor. The exam time keeps running.</strong></p>
  <p>{status?.status === 'grace' ? 'Grace time remaining' : 'Remaining time'} (last server report): <strong>{remainingTime(status)}</strong>.</p>
  {#if timingStale}<p role="alert">Timing stale / disconnected — waiting for a current server report.</p>{/if}
  <p>No questions are shown while the exam is locked. Your unsaved answers are kept and sent once your instructor unlocks the exam.</p>
</Card>
