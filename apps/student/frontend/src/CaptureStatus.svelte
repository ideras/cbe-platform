<script>
  import Card from '../../../../shared/theme/Card.svelte';
  let { capture, admission = false, testable = false, capturing = false, captureTest = { state: 'idle' }, busy = false, onTest, onCancel } = $props();
</script>

{#if admission || testable || capture?.enabled}
  <Card label="Screenshot monitoring" title={testable ? 'Screenshot test' : 'Screen capture and retention'} level={3} tone={testable ? 'info' : 'deep'}>
    {#if testable}
      <p>Take one test screenshot to check that capture works on this computer. It is not saved or sent.</p>
      <div class="actions">
        <button type="button" onclick={onTest} disabled={capturing || busy || Boolean(capture?.pending_reports)}>{capturing ? 'Testing screenshot capture…' : 'Test screenshot capture'}</button>
        {#if captureTest.state === 'running'}<button type="button" onclick={onCancel}>Cancel capture</button>{/if}
      </div>
      <p aria-label="Screenshot test result" role={captureTest.state === 'failed' ? 'alert' : 'status'} class="callout" class:success={captureTest.state === 'passed' && capture?.ready}
        class:pending={captureTest.state !== 'passed' && captureTest.state !== 'failed'}>
        {#if captureTest.state === 'passed' && capture?.ready}Screenshot capture succeeded ({captureTest.width} × {captureTest.height}). Test image discarded; nothing uploaded.
        {:else if captureTest.state === 'running'}Capturing… approve the system dialog if one appears.
        {:else if captureTest.state === 'failed'}Screenshot capture failed. {captureTest.error}
        {:else if captureTest.state === 'cancelled'}Screenshot test cancelled. No image saved.
        {:else}Required before you can log in.{/if}
      </p>
      {#if capture?.ready && capture.pending_reports}
        <p role="note" class="callout warning">Screenshot test passed. Waiting for the server to record it.</p>
      {:else if !capture?.ready && capture?.permission?.message}
        <p role="note" class="callout warning">{capture.permission.message}</p>
      {/if}
      {#if captureTest.note}<p role="note" class="callout warning">{captureTest.note}</p>{/if}
    {:else}
      <p>{capture.permission.message}</p>
      {#if !capture.permission.supported || !capture.permission.granted}<p role="alert">Capture support/permission is not confirmed. Notify the instructor; do not assume monitoring is complete.</p>{/if}
      <p>Capture state: {capture.state}. Pending images: {capture.pending}. Local incident: {capture.local_incident || 'none'}. Loss event: {capture.event_log_id || 'not acknowledged'}.</p>
      {#if capture.screenshot_id}<p>Last confirmed screenshot: {capture.screenshot_id}. This is not an answer-save acknowledgment.</p>{/if}
      <p>Images are memory-only before bounded upload; shutdown, failure or timeout releases them. Proctors purge server images after grading; backups/exports require separate deletion.</p>
    {/if}
    {#if capture?.error}<p role="alert">{capture.error}</p>{/if}
    {#if capture?.failures}<p role="alert">Unconfirmed/failed screenshot incidents: {capture.failures}. Instructor follow-up is required.</p>{/if}
    {#if capture?.pending_reports}<p role="status" class="callout pending">Capture status reports awaiting server acknowledgment: {capture.pending_reports}. Editing is paused until they are acknowledged; keep the app open.</p>{/if}
    {#if capture?.report_error}<p role="alert">{capture.report_error}</p>{/if}
  </Card>
{/if}

<style>
  p { overflow-wrap: anywhere; }
  .actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin: var(--space-3) 0; }
  button { max-width: 100%; margin: 0; }
</style>
