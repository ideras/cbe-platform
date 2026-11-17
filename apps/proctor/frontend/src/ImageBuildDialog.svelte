<script>
  import { onMount, onDestroy, untrack, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { imageChip, shortDigest, buildTime, buildLine, buildFailureLabel, stepProgress, outputAgo, atEnd } from './workspace.js';
  // The image of a workspace exam that has its own (docs/workspace/images.md): its state, a button to build it, and the log of the build,
  // which is what the builder printed: the base image's digest, the steps, what verification said. The build goes on in the background and
  // takes seconds to minutes. While it does, the dialog reads it every second, asking only for the log from where the last look ended, and
  // shows it as it grows with where the build is (step 4 of 9) and when it last printed anything. The log is text, shown as text.
  // The exam is the entry the proctor chose; the dialog keeps it, and onChanged tells the exam list to read the catalog again.
  let { entry, locked = false, onClose, onChanged } = $props();
  const exam = untrack(() => entry);

  let view = $state(null);
  let problem = $state('');
  let working = $state(false);
  // The build whose log is shown: the latest by default, or one the proctor picked from the list.
  let picked = $state(null);
  let pickedBuild = $state(null);
  let now = $state(Date.now());
  let timer, clock, alive = true;
  // The log of a build that was asked for by number is read once: it does not change, unlike the latest build's.
  const POLL_MS = 1000;
  // The log of the latest build as it has been read: its text, the build it is the log of and the offset to ask for next. A build that is going is
  // read from the offset; one that has ended is read whole.
  let latestLog = $state('');
  let latestBuild = 0;
  let nextOffset = 0;
  // Whether the log box keeps its end in view as the log grows; scrolling up to read stops it, and the box below turns it back on.
  let follow = $state(true);
  let box = $state(null);

  const going = $derived(view?.state === 'queued' || view?.state === 'building');
  const chip = $derived(view ? imageChip({ state: view.state, ready: view.ready }) : null);
  const build = $derived(pickedBuild ?? view?.build ?? null);
  const log = $derived(pickedBuild ? pickedBuild.log ?? '' : latestLog);
  const progress = $derived(view?.state === 'building' ? stepProgress(view.build) : null);
  const quiet = $derived(view?.state === 'building' ? outputAgo(view.build?.last_output_at, now) : '');
  const elapsed = $derived.by(() => {
    if (view?.state !== 'building' || !view.build?.started_at) return '';
    return buildTime(Math.max(0, now - Date.parse(view.build.started_at)));
  });

  async function read({ quiet = false } = {}) {
    try {
      // From where the last look ended, if what is going is the build that look was of; otherwise all of it.
      const asked = view?.build?.build_id === latestBuild && view?.build?.live ? nextOffset : 0;
      const next = await api.ExamImage(exam.exam_id, true, asked);
      if (!alive) return;
      // The exam list reads the catalog again when the state of the image changes, not at every look.
      const changed = view?.state !== next.state || view?.build?.build_id !== next.build?.build_id;
      const latest = next.build;
      if (latest) {
        // What came starts where the look asked for it: it goes on from the text there is. If it starts elsewhere (the server did not have the offset),
        // or this is another build, it is all of the log there is.
        const goesOn = latest.build_id === latestBuild && latest.live && latest.log_from === asked && asked > 0;
        latestLog = goesOn ? latestLog + (latest.log ?? '') : latest.log ?? '';
        latestBuild = latest.build_id;
        nextOffset = latest.log_next ?? 0;
      } else { latestLog = ''; latestBuild = 0; nextOffset = 0; }
      view = next;
      if (!quiet) problem = '';
      if (changed) onChanged?.();
    } catch (err) { if (alive) problem = String(err); }
  }
  function schedule() {
    clearTimeout(timer);
    if (!alive) return;
    timer = setTimeout(async () => { if (going) await read({ quiet: true }); schedule(); }, POLL_MS);
  }
  onMount(async () => {
    await read();
    schedule();
    clock = setInterval(() => now = Date.now(), 1000);
  });
  onDestroy(() => { alive = false; clearTimeout(timer); clearInterval(clock); });

  async function start(rebuild) {
    if (working || going) return;
    working = true; problem = ''; picked = null; pickedBuild = null;
    try {
      view = await api.BuildExamImage(exam.exam_id, rebuild);
      onChanged?.();
      await read();
    } catch (err) { problem = String(err); await read({ quiet: true }); }
    finally { working = false; }
  }
  // A log that grows is kept in view, unless the proctor scrolled up to read it.
  $effect(() => {
    latestLog;
    if (follow && box) tick().then(() => { if (box && follow) box.scrollTop = box.scrollHeight; });
  });
  const scrolled = () => { if (box) follow = atEnd(box); };
  async function pick(event) {
    const id = Number(event.currentTarget.value);
    problem = '';
    if (!id || id === view?.build?.build_id) { picked = null; pickedBuild = null; return; }
    picked = id;
    try { pickedBuild = await api.ExamImageBuild(exam.exam_id, id); } catch (err) { problem = String(err); picked = null; pickedBuild = null; }
  }
  const showTime = ms => buildTime(ms);
</script>

<Modal title="Image of {exam.title}" titleId="image-dialog-title" icon="server" subtitle="The exam’s own image, built from {view?.image || 'the base its Containerfile names'} before the exam is published." width="46rem" busy={working} {onClose}>
  <div class="image" data-testid="image-dialog">
    {#if !view && !problem}<p class="muted" role="status">Reading the image…</p>{/if}
    {#if view}
      <p class="state" role="status" aria-live="polite">
        {#if chip}<span class="chip tone-{chip.tone}">{chip.text}</span>{/if}
        {#if view.state === 'building' && elapsed}<span class="elapsed">{elapsed}</span>{/if}
        {#if view.state === 'ready' && view.build?.cached}<span class="note">The same files on the same base were built already: nothing was built again.</span>{/if}
      </p>
      <p class="why">A student’s workspace and the hidden tests of this exam start from this image. Students’ containers never have network, whatever the build had. Building uses the network of this server and the course image the server has now.</p>
      {#if view.state === 'failed' && view.build}
        <div class="failure" role="alert"><strong>{buildFailureLabel(view.build.failure)}.</strong> {view.build.message}
          {#if view.ready}<span class="kept">An earlier build of the same files is still the exam’s image.</span>{/if}</div>
      {/if}
      {#if view.state === 'missing'}<div class="failure" role="alert"><strong>The runner does not have this exam’s image any more.</strong> Build it again before publishing.</div>{/if}
      {#if view.build}
        <dl>
          <div><dt>{view.own_base ? 'Base image' : 'Course image'}</dt><dd>{view.build.base_name || view.image}</dd></div>
          {#if view.build.base_digest}<div><dt>Built on</dt><dd><code title={view.build.base_digest}>{shortDigest(view.build.base_digest)}</code></dd></div>{/if}
          {#if view.build.hash}<div><dt>Image</dt><dd><code title="localhost/cbe-exam/{exam.exam_id}:{view.build.hash}">{view.build.hash}</code></dd></div>{/if}
          {#if view.build.state === 'ok' || view.build.state === 'failed'}<div><dt>Took</dt><dd>{showTime(view.build.duration_ms)}</dd></div>{/if}
        </dl>
        {#if view.build.warnings?.length}
          <div class="warnings"><strong>Warnings</strong>
            <ul>{#each view.build.warnings as warning}<li>{warning}</li>{/each}</ul>
            <p class="hint">Warnings do not stop the exam from being published. A package installed without a version may not give the same image when it is built again.</p>
          </div>
        {/if}
      {/if}

      <div class="buttons">
        <button type="button" class="primary" disabled={locked || working || going} onclick={() => start(false)}>
          <Icon name={view.state === 'not_built' ? 'play' : 'refresh'} size={16} />{going ? 'Building…' : view.state === 'not_built' ? 'Build the image' : view.ready ? 'Build again' : 'Try the build again'}
        </button>
        {#if view.ready || view.state === 'failed'}
          <button type="button" disabled={locked || working || going} title="Build without the runner’s cache: every step runs again" onclick={() => start(true)}>Build again from scratch</button>
        {/if}
        <button type="button" disabled={working} onclick={() => read()}>Refresh</button>
        <button type="button" disabled={working} onclick={onClose}>Close</button>
      </div>

      {#if view.builds?.length > 1}
        <label class="pick" for="image-build-pick"><span>Log of</span>
          <select id="image-build-pick" value={picked ?? view.build?.build_id} onchange={pick}>
            {#each view.builds as item (item.build_id)}<option value={item.build_id}>{buildLine(item)}</option>{/each}
          </select>
        </label>
      {/if}
      {#if build}
        <section aria-labelledby="image-log-title">
          <h3 id="image-log-title">Build log{#if build.build_id}{' · '}build #{build.build_id}{/if}</h3>
          {#if progress}
            <div class="progress" data-testid="build-progress">
              <div class="progress-line"><strong>{progress.text}</strong><code>{progress.command}</code>{#if quiet}<span class="quiet">Last output {quiet}</span>{/if}</div>
              <div class="bar" role="progressbar" aria-label="Build progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow={progress.percent}><span style:width="{progress.percent}%"></span></div>
            </div>
          {/if}
          {#if log}
            <pre class="log" tabindex="0" aria-label="Build output" bind:this={box} onscroll={scrolled}>{log}</pre>
            {#if !pickedBuild && going}<label class="follow"><input type="checkbox" bind:checked={follow} onchange={() => { if (follow && box) box.scrollTop = box.scrollHeight; }} /> Follow the end of the log</label>{/if}
          {:else if build.state === 'queued'}<p class="muted">The build is waiting for its turn: the server builds one image at a time.</p>
          {:else if build.state === 'running'}<p class="muted">The build has started and has not printed anything yet.</p>
          {:else}<p class="muted">This build has no log.</p>{/if}
        </section>
      {:else}
        <p class="muted">Nothing has been built yet. The first build takes seconds when the Containerfile only copies files, and minutes when it installs packages.</p>
      {/if}
    {/if}
    {#if problem}<p role="alert" class="problem">{problem}</p>{/if}
  </div>
</Modal>

<style>
  .image { display: grid; gap: .8rem; padding: .25rem 0 .5rem; min-width: 0; }
  p { margin: 0; }
  .state { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; }
  .chip { display: inline-block; padding: .15rem .7rem; border: 1px solid var(--border-subtle); border-radius: 999px; font-weight: 700; font-size: .9rem; }
  .tone-ok { background: var(--ok-fill); color: var(--ok-text); border-color: var(--ok-border); }
  .tone-warn { background: var(--warn-fill); color: var(--warn-text); }
  .tone-active { background: var(--selection-fill); color: var(--action-hover); border-color: var(--cyan-border); }
  .tone-lock { background: var(--alert-fill); color: var(--alert-text); border-color: var(--alert); }
  .elapsed { font-family: var(--font-mono); color: var(--text-muted); }
  .note, .hint, .muted { color: var(--text-muted); font-size: .85rem; }
  .why { color: var(--text-muted); font-size: .88rem; }
  .failure { padding: .55rem .75rem; border: 1px solid var(--alert); border-radius: 9px; background: var(--alert-fill); color: var(--alert-text); overflow-wrap: anywhere; }
  .kept { display: block; margin-top: .3rem; font-size: .85rem; }
  dl { display: grid; grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr)); gap: .5rem .9rem; margin: 0; }
  dl > div { min-width: 0; }
  dt { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  dd { margin: .1rem 0 0; font-weight: 600; overflow-wrap: anywhere; }
  code { font-family: var(--font-mono); font-size: .85rem; }
  .warnings { padding: .55rem .75rem; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); }
  .warnings ul { margin: .3rem 0 .3rem 1.1rem; padding: 0; }
  .buttons { display: flex; flex-wrap: wrap; gap: .5rem; }
  .buttons button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; }
  .pick { display: flex; align-items: center; gap: .5rem; font-size: .88rem; }
  .pick select { min-width: 0; max-width: 100%; }
  h3 { margin: 0 0 .3rem; font-size: .95rem; }
  .progress { display: grid; gap: .35rem; margin-bottom: .5rem; }
  .progress-line { display: flex; flex-wrap: wrap; align-items: baseline; gap: .2rem .7rem; }
  .progress-line code { min-width: 0; overflow-wrap: anywhere; color: var(--text-muted); }
  .quiet { margin-left: auto; color: var(--text-muted); font-size: .82rem; }
  .bar { height: 7px; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .bar span { display: block; height: 100%; border-radius: inherit; background: var(--bar-fill, var(--action-primary)); transition: width .4s ease; }
  .follow { display: inline-flex; align-items: center; gap: .4rem; margin-top: .35rem; font-size: .85rem; color: var(--text-muted); }
  .log { max-height: 20rem; margin: 0; padding: .6rem .75rem; overflow: auto; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--bg-primary); color: var(--text-main); font-family: var(--font-mono); font-size: .78rem; line-height: 1.4; white-space: pre-wrap; overflow-wrap: anywhere; }
  .problem { color: var(--alert-text); overflow-wrap: anywhere; }
</style>
