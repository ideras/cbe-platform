<script>
  import Card from '../../../../shared/theme/Card.svelte';
  // admission: what the login's screen capture check is, before the student logs in. testable: the retest of a running exam.
  // The errors listed here are also announced as toasts, so they carry no live role of their own.
  let { capture, admission = false, checking = false, testable = false, capturing = false, captureTest = { state: 'idle' }, busy = false, onTest, onCancel } = $props();
</script>

{#if admission || testable || capture?.enabled}
  <Card label="Screenshot monitoring" title={testable ? 'Screenshot test' : admission ? 'Screen capture check' : 'Screen capture and retention'} level={3} tone={testable || admission ? 'info' : 'deep'}>
    {#if admission}
      <p>When you click <strong>Login / resume</strong>, the app takes one test screenshot to check that capture works on this computer. The image is not saved or sent. The exam does not start unless the check succeeds.</p>
      <p>If your system asks for permission to record or share your screen, allow it.</p>
      {#if checking}<p class="callout pending">Checking screen capture…</p>
      {:else if !capture?.ready && capture?.permission?.message}<p class="callout warning">{capture.permission.message}</p>{/if}
    {:else if testable}
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
        {:else}Required before editing resumes.{/if}
      </p>
      {#if capture?.ready && capture.pending_reports}
        <p role="note" class="callout warning">Screenshot test passed. Waiting for the server to record it.</p>
      {:else if !capture?.ready && capture?.permission?.message}
        <p role="note" class="callout warning">{capture.permission.message}</p>
      {/if}
      {#if captureTest.note}<p role="note" class="callout warning">{captureTest.note}</p>{/if}
    {:else}
      <p>{capture.permission.message}</p>
      {#if !capture.permission.supported || !capture.permission.granted}<p>Capture support/permission is not confirmed. Notify the instructor; do not assume monitoring is complete.</p>{/if}
      <p>Capture state: {capture.state}. Pending images: {capture.pending}. Local incident: {capture.local_incident || 'none'}. Loss event: {capture.event_log_id || 'not acknowledged'}.</p>
      {#if capture.screenshot_id}<p>Last confirmed screenshot: {capture.screenshot_id}. This is not an answer-save acknowledgment.</p>{/if}
      <p>Images are memory-only before bounded upload; shutdown, failure or timeout releases them. Proctors purge server images after grading; backups/exports require separate deletion.</p>
    {/if}
    {#if capture?.error}<p>{capture.error}</p>{/if}
    {#if capture?.failures}<p>Unconfirmed/failed screenshot incidents: {capture.failures}. Instructor follow-up is required.</p>{/if}
    {#if capture?.pending_reports}<p role="status" class="callout pending">Capture status reports awaiting server acknowledgment: {capture.pending_reports}. Editing is paused until they are acknowledged; keep the app open.</p>{/if}
    {#if capture?.report_error}<p>{capture.report_error}</p>{/if}
  </Card>
{/if}

<style>
  p { overflow-wrap: anywhere; }
  .actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin: var(--space-3) 0; }
  button { max-width: 100%; margin: 0; }
</style>
