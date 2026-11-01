<script>
  import * as api from '$proctor-api';
  let { value = $bindable(''), exam = null, serverTime = null } = $props();
  let preview = $state(null);
  let problem = $state('');
  $effect(() => {
    const time = value;
    let live = true;
    preview = null; problem = '';
    if (time) api.PreviewEndTime(time).then(result => { if (live) preview = result; }).catch(err => { if (live) problem = String(err); });
    return () => { live = false; };
  });
  const allotted = $derived(preview && exam ? Math.max(0, Math.floor((new Date(preview.ends_at) - new Date(serverTime || Date.now())) / 60000)) : null);
</script>
<label>Exam ends at (optional, local time) <input type="time" bind:value /></label>
{#if preview}
  <p>{preview.label}</p>
  {#if preview.clock_warning}<p class="callout warning">The server and this computer's clocks differ by more than one minute. Check the end time carefully.</p>{/if}
  {#if allotted < exam?.duration_seconds / 60}<p class="callout warning">A student who logs in now gets {allotted} of {exam.duration_seconds / 60} minutes.</p>{/if}
  {#if exam?.opens_at && new Date(preview.ends_at) <= new Date(exam.opens_at)}<p class="callout warning">This is before admission opens: nobody can take the exam.</p>{/if}
  {#if exam?.closes_at && new Date(preview.ends_at) > new Date(exam.closes_at)}<p class="muted">This is after admission closes. Running sessions can continue until the end time.</p>{/if}
{/if}
{#if problem}<p role="alert">{problem}</p>{/if}
<style>label { display: block; margin-block: var(--space-3); } input { margin-inline-start: var(--space-2); }</style>
