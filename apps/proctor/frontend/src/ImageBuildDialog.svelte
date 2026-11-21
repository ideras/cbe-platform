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
  let reading = $state(false);
  let fromScratch = $state(false);
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
  let logOpen = $state(null);
  let copyFeedback = $state('');
  let image;
  let copyTimer;

  const going = $derived(view?.state === 'queued' || view?.state === 'building');
  const canChooseScratch = $derived(view?.ready || view?.state === 'failed');
  const buildDisabled = $derived(locked || working || reading || going);
  const chip = $derived(view ? imageChip({ state: view.state, ready: view.ready }) : null);
  const build = $derived(picked ? pickedBuild ?? view?.builds?.find(item => item.build_id === picked) : view?.build);
  const log = $derived(picked ? pickedBuild?.log ?? '' : latestLog);
  const progress = $derived(view?.state === 'building' ? stepProgress(view.build) : null);
  const quiet = $derived(view?.state === 'building' ? outputAgo(view.build?.last_output_at, now) : '');
  const elapsed = $derived.by(() => {
    if (view?.state !== 'building' || !view.build?.started_at) return '';
    return buildTime(Math.max(0, now - Date.parse(view.build.started_at)));
  });

  async function read({ quiet = false } = {}) {
    if (reading) return;
    reading = true;
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
      if (going && next.build?.build_id === view.build?.build_id && next.build?.state === 'ok') logOpen ??= true;
      view = next;
      if (!quiet) problem = '';
      if (changed) onChanged?.();
    } catch (err) { if (alive) problem = String(err); }
    finally { reading = false; }
  }
  function schedule() {
    clearTimeout(timer);
    if (!alive) return;
    timer = setTimeout(async () => { if (going) await read({ quiet: true }); schedule(); }, POLL_MS);
  }
  onMount(async () => {
    clock = setInterval(() => now = Date.now(), 1000);
    await read();
    schedule();
  });
  onDestroy(() => { alive = false; clearTimeout(timer); clearInterval(clock); clearTimeout(copyTimer); });

  async function start(rebuild) {
    if (buildDisabled) return;
    working = true; problem = '';
    try {
      const next = await api.BuildExamImage(exam.exam_id, rebuild);
      fromScratch = false;
      if (next.build?.build_id !== latestBuild) { latestLog = ''; latestBuild = 0; nextOffset = 0; }
      picked = null; pickedBuild = null; follow = true; logOpen = true;
      view = next;
      onChanged?.();
      await read();
    } catch (err) { problem = String(err); await read({ quiet: true }); }
    finally { working = false; }
  }
  // A log that grows is kept in view, unless the proctor scrolled up to read it.
  $effect(() => {
    latestLog;
    if (!picked && follow && box) tick().then(() => { if (!picked && box && follow) box.scrollTop = box.scrollHeight; });
  });
  const scrolled = () => { if (box && !picked && going) follow = atEnd(box); };
  async function pick(event) {
    const id = Number(event.currentTarget.value);
    problem = ''; logOpen = true; pickedBuild = null;
    if (!id || id === view?.build?.build_id) { picked = null; return; }
    picked = id;
    try {
      const result = await api.ExamImageBuild(exam.exam_id, id);
      if (alive && picked === id) pickedBuild = result;
    } catch (err) {
      if (alive && picked === id) { problem = String(err); picked = null; }
    }
  }

  const latest = $derived(view?.build);
  const baseName = $derived(latest?.base_name || view?.image);
  const imageRef = $derived(latest?.hash ? `localhost/cbe-exam/${exam.exam_id}:${latest.hash}` : '');
  const finished = $derived(latest?.state === 'ok' || latest?.state === 'failed');
  const duration = $derived(finished ? buildTime(latest.duration_ms) : '');
  const builtAt = $derived(latest?.finished_at ? new Date(latest.finished_at).toLocaleString() : '');
  const showLog = $derived(logOpen ?? (build?.state !== 'ok'));
  const lines = $derived(log ? log.replace(/\n$/, '').split('\n').length : 0);
  const logParts = $derived.by(() => {
    if (!showLog || !log) return [];
    // Color familiar runner lines, without interpreting any of the output as HTML.
    const parts = [];
    let offset = 0;
    const markers = /^(?:(STEP \d+\/\d+:)|(Verified:|Successfully |[ \t]*self test[ \t]+ok\b)|(Not verified:|[ \t]*self test[ \t]+FAIL\b)|(-->|COMMIT\b)).*/gm;
    for (const line of log.matchAll(markers)) {
      if (line.index > offset) parts.push({ text: log.slice(offset, line.index) });
      parts.push({ text: line[0], tone: line[1] ? 'step-line' : line[2] ? 'ok-line' : line[3] ? 'fail-line' : 'dim-line' });
      offset = line.index + line[0].length;
    }
    if (offset < log.length) parts.push({ text: log.slice(offset) });
    return parts;
  });
  const tone = $derived(view?.state === 'ready' ? 'ready' : going ? 'active' : chip?.tone === 'lock' || view?.state === 'failed' ? 'failed' : 'neutral');
  const summary = $derived({
    ready: 'Students can use this image',
    not_built: 'Build the image before publishing the exam',
    queued: 'The build is queued on the server',
    building: 'The server is building the exam image',
    failed: view?.ready ? 'The previous image is still in use' : 'The exam can’t be published until a build passes',
    missing: 'Build the image again before publishing',
    none: 'This exam uses its course image',
  }[view?.state]);

  // A completed own-base build's self-test report is the block just before the runner's final message.
  // Read that block only, not incidental matches earlier in the (untrusted) Containerfile output.
  function reportedChecks(ownBase, build, log) {
    if (!ownBase || !build || build.cached) return [];
    let output = log.trimEnd();
    let end;
    if (build.state === 'ok') end = output.match(/\n\nVerified: student [^\n]+$/)?.index;
    else if (build.failure === 'verify' && build.message?.startsWith('the image does not keep the contract of a workspace image:')) {
      const suffix = `\n\n${build.message}`;
      if (output.endsWith(suffix)) end = output.length - suffix.length;
    }
    if (end === undefined) return [];
    output = output.slice(0, end);
    const rows = output.split('\n');
    const checks = [];
    for (let i = rows.length - 1; i >= 0; i--) {
      const check = rows[i].match(/^[ \t]*self test[ \t]+(ok|FAIL)[ \t]+([\w-]+)$/);
      if (!check) break;
      checks.push({ name: check[2], ok: check[1] === 'ok' });
    }
    return checks.reverse();
  }
  const latestChecks = $derived(reportedChecks(view?.own_base, latest, latestLog));
  const checks = $derived(picked ? reportedChecks(view?.own_base, pickedBuild, log) : latestChecks);
  const failedChecks = $derived(checks.filter(check => !check.ok).length);
  const phase = $derived.by(() => {
    if (view?.state === 'building') {
      if (latest?.step || /^Building (?:the exam's image|localhost\/cbe-exam\/)/m.test(latestLog)) return 'Build';
      return 'Base image';
    }
    if (view?.state !== 'failed') return '';
    switch (latest?.failure) {
      case 'base': case 'containerfile': case 'policy': return 'Base image';
      case 'build': case 'timeout': return 'Build';
      case 'verify': return latestChecks.some(check => !check.ok) ? 'Self tests' : 'Verify';
      default: return '';
    }
  });
  const stages = $derived(view?.own_base ? ['Base image', 'Build', 'Self tests', 'Verify'] : ['Base image', 'Build', 'Verify']);

  async function copy(value, label) {
    let copied = false;
    const button = document.activeElement;
    try { await navigator.clipboard.writeText(value); copied = true; } catch {}
    // Some desktop webviews do not expose the Clipboard API. Keep the fallback inside the native dialog.
    if (!alive) return;
    if (!copied) {
      const field = document.createElement('textarea');
      field.value = value; field.readOnly = true; field.style.position = 'fixed'; field.style.opacity = '0';
      image.append(field);
      field.select();
      try { copied = document.execCommand('copy'); } catch {}
      field.remove();
      button?.focus?.();
    }
    copyFeedback = copied ? `${label} copied.` : `Could not copy ${label.toLowerCase()}.`;
    clearTimeout(copyTimer);
    copyTimer = setTimeout(() => copyFeedback = '', 2500);
  }
</script>

{#snippet copyButton(value, label)}
  <button type="button" class="small-button" aria-label="Copy {label.toLowerCase()}" onclick={() => copy(value, label)}>
    <Icon name="copy" size={14} /> Copy
  </button>
{/snippet}

<Modal title={exam.title} titleId="image-dialog-title" eyebrow="Exam image" icon="server" tone="deep" width="45rem" busy={working} {onClose}>
  {#snippet footer()}
    <div class="actions">
      <div class="secondary-actions">
        <button type="button" class="ghost" disabled={working} onclick={onClose}>Close</button>
        <button type="button" class="ghost" disabled={working || reading} title="Reload the status from the server" onclick={() => read()}>
          <Icon name="refresh" size={16} /> Refresh
        </button>
      </div>
      {#if view && view.state !== 'none'}
        <div class="build-actions">
          <div class="action-row">
            {#if canChooseScratch}
              <label class="scratch-option">
                <input type="checkbox" bind:checked={fromScratch} disabled={buildDisabled} /> From scratch
              </label>
            {/if}
            <button type="button" class="primary" disabled={buildDisabled} onclick={() => start(canChooseScratch && fromScratch)}>
              {#if going}<span class="spin" aria-hidden="true"></span>{:else}<Icon name={view.state === 'not_built' ? 'play' : 'refresh'} size={16} />{/if}
              {going ? 'Building…' : view.state === 'not_built' ? 'Build the image' : view.ready ? 'Build again' : 'Try the build again'}
            </button>
          </div>
          {#if locked}<p class="hint">Building is unavailable while the exam list is locked.</p>
          {:else if !going && canChooseScratch}<p class="hint">Check “From scratch” to run every step without the build cache.</p>{/if}
        </div>
      {/if}
    </div>
    <div aria-live="polite" aria-atomic="true">
      {#if copyFeedback}<p class="copy-feedback">{copyFeedback}</p>{/if}
    </div>
  {/snippet}

  <div class="image" data-testid="image-dialog" bind:this={image}>
    {#if !view && !problem}<p class="muted" role="status">Reading the image…</p>{/if}
    {#if view}
      <div class="hero {tone}">
        <span class="badge">
          {#if going}<span class="spin" aria-hidden="true"></span>{:else}<Icon name={tone === 'ready' ? 'check' : tone === 'failed' ? 'close' : 'server'} size={20} />{/if}
        </span>
        <div class="hero-text" aria-live="polite" aria-atomic="true">
          <p class="hero-label">{chip?.text || 'Course image'}</p>
          <h3>{summary}</h3>
          {#if view.state === 'ready' && builtAt}<p>Built {builtAt} · verified</p>{/if}
          {#if view.state === 'ready' && latest?.cached}<p>The same files and base already had a verified image. Nothing was built again.</p>{/if}
        </div>
        {#if latest}
          <div class="chips">
            <span class="chip">Build #{latest.build_id}</span>
            {#if elapsed || duration}<span class="chip"><Icon name="clock" size={13} /> {elapsed || duration}</span>{/if}
            {#if latest.rebuild}<span class="chip">From scratch</span>{/if}
          </div>
        {/if}
      </div>

      {#if going || phase}
        <div class="build-progress">
          <ol class="stages" aria-label="Build stages">
            {#each stages as stage, i (stage)}
              {@const current = phase === stage}
              {@const done = i < stages.indexOf(phase)}
              <li class:done class:current={current && going} class:failed={current && !going} aria-current={current && going ? 'step' : undefined}>
                <span class="stage-mark" aria-hidden="true">
                  {#if done}<Icon name="check" size={12} />{:else if current && !going}<Icon name="close" size={12} />{/if}
                </span>
                {stage}{#if done}<span class="sr-only">{' complete'}</span>{:else if current && !going}<span class="sr-only">{' failed'}</span>{/if}
              </li>
            {/each}
          </ol>
          {#if progress}
            <div class="progress" data-testid="build-progress">
              <div class="progress-line"><strong>{progress.text}</strong><code>{progress.command}</code>{#if quiet}<span class="quiet">Last output {quiet}</span>{/if}</div>
              <div class="bar" role="progressbar" aria-label="Build progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow={progress.percent}><span style:width="{progress.percent}%"></span></div>
            </div>
          {/if}
        </div>
      {/if}
      {#if view.state === 'failed' && latest}
        <div class="failure" role="alert"><strong>{buildFailureLabel(latest.failure)}.</strong> {latest.message}
          {#if view.ready}<p class="hint">An earlier build of the same files is still the exam’s image.</p>{/if}
        </div>
      {:else if view.state === 'missing'}
        <div class="failure" role="alert"><strong>The runner does not have this exam’s image any more.</strong> Build it again before publishing.</div>
      {/if}

      <section aria-labelledby="image-use-title">
        <h3 class="section-label" id="image-use-title">Used for</h3>
        <p class="muted usage">Each student’s workspace and this exam’s hidden tests start from this image. It is built from {view.image || 'the base its Containerfile names'} before the exam is published.</p>
        <div class="network">
          <div class="network-card students">
            <span class="network-icon"><Icon name="wifiOff" size={16} /></span>
            <div><strong>Students’ containers never have network</strong><p>They run offline regardless of how the image was built.</p></div>
          </div>
          <div class="network-card builder">
            <span class="network-icon"><Icon name="server" size={16} /></span>
            <div><strong>Build: this server’s network</strong><p>{view.own_base ? 'Uses the base named in the Containerfile.' : 'Uses the course image the server has now.'}</p></div>
          </div>
        </div>
      </section>

      {#if latest && (baseName || finished)}
        <section aria-labelledby="image-facts-title">
          <h3 class="section-label" id="image-facts-title">Image</h3>
          <dl class="facts">
            {#if baseName}
              <div class="fact"><dt>{view.own_base ? 'Base image' : 'Course image'}</dt><dd>
                <div><strong>{baseName}</strong>
                  {#if latest.base_digest}<code class="sub" title={latest.base_digest}>{shortDigest(latest.base_digest)}</code>{/if}
                </div>
                {@render copyButton(latest.base_digest || baseName, latest.base_digest ? 'Base digest' : 'Base image name')}
              </dd></div>
            {/if}
            {#if latest.state === 'ok' && imageRef}
              <div class="fact"><dt>Exam image</dt><dd>
                <div><code class="tag" title={imageRef}>{latest.hash}</code><code class="sub" title={imageRef}>{imageRef}</code></div>
                {@render copyButton(imageRef, 'Exam image reference')}
              </dd></div>
            {/if}
            {#if finished}
              <div class="fact"><dt>Last build</dt><dd>
                <div class="chips"><span class="chip">{duration}</span>{#if builtAt}<span class="chip">{builtAt}</span>{/if}
                  {#if latest.cached}<span class="chip">Already built</span>{:else if latest.rebuild}<span class="chip">From scratch</span>{/if}
                </div>
              </dd></div>
            {/if}
          </dl>
        </section>
      {/if}
      {#if latest?.warnings?.length}
        <div class="warnings"><strong>Warnings</strong>
          <ul>{#each latest.warnings as warning}<li>{warning}</li>{/each}</ul>
          <p class="hint">Warnings do not stop the exam from being published. A package installed without a version may change the image on the next build.</p>
        </div>
      {/if}

      {#if checks.length}
        <section aria-labelledby="image-checks-title">
          <h3 class="section-label" id="image-checks-title">Self tests{#if picked} · build #{build.build_id}{/if}
            <span class="aside">{checks.length - failedChecks}/{checks.length} passed</span>
          </h3>
          <ul class="checks" aria-label="Self tests reported in the build log">
            {#each checks as check}
              <li class:failed={!check.ok}><Icon name={check.ok ? 'check' : 'close'} size={15} /><span>{check.name}<span class="sr-only">{check.ok ? ' passed' : ' failed'}</span></span></li>
            {/each}
          </ul>
        </section>
      {/if}
      {#if build?.state === 'ok' && !build.cached && view.state !== 'missing'}
        <p class="verified"><Icon name="shieldCheck" size={18} /> <span><strong>Verified.</strong> This build passed the runner’s workspace image checks.</span></p>
      {/if}

      {#if build}
        <section aria-labelledby="image-log-title">
          <div class="log-label">
            <h3 class="section-label" id="image-log-title">Build log</h3>
            {#if view.builds?.length > 1}
              <label class="pick" for="image-build-pick"><span>Log of</span>
                <select id="image-build-pick" value={picked ?? latest?.build_id} onchange={pick}>
                  {#each view.builds as item (item.build_id)}<option value={item.build_id}>{buildLine(item)}</option>{/each}
                </select>
              </label>
            {/if}
          </div>
          <div class="terminal">
            <div class="log-head">
              <h4>Build #{picked ?? build.build_id}</h4>
              {#if lines && (!picked || pickedBuild)}<span class="lines">{lines} {lines === 1 ? 'line' : 'lines'}</span>{/if}
              {#if log && (!picked || pickedBuild)}
                <div class="log-actions">
                  {@render copyButton(log, 'Build log')}
                  <button type="button" class="small-button" aria-expanded={showLog} aria-controls="image-build-output" onclick={() => logOpen = !showLog}>{showLog ? 'Hide' : 'Show'}</button>
                </div>
              {/if}
            </div>
            <div id="image-build-output">
              {#if picked && !pickedBuild}<p class="log-note" role="status">Reading the build log…</p>
              {:else if log}
                {#if showLog}
                  {#key build.build_id}
                    <!-- svelte-ignore a11y_no_noninteractive_tabindex (keyboard users need to scroll the build output) -->
                    <pre class="log" tabindex="0" aria-label="Build output" bind:this={box} onscroll={scrolled}>{#each logParts as part}{#if part.tone}<span class={part.tone}>{part.text}</span>{:else}{part.text}{/if}{/each}</pre>
                  {/key}
                  {#if !picked && going}<label class="follow"><input type="checkbox" bind:checked={follow} /> Follow the end of the log</label>{/if}
                {/if}
              {:else if build.state === 'queued'}<p class="log-note">The build is waiting for its turn: the server builds one image at a time.</p>
              {:else if build.state === 'running'}<p class="log-note">The build has started and has not printed anything yet.</p>
              {:else}<p class="log-note">This build has no log.</p>{/if}
            </div>
          </div>
        </section>
      {:else if view.state === 'not_built'}
        <p class="muted">Nothing has been built yet. The first build takes seconds when the Containerfile only copies files, and minutes when it installs packages.</p>
      {/if}
    {/if}
    {#if problem}<p role="alert" class="failure">{problem}</p>{/if}
  </div>
</Modal>

<style>
  .image { display: grid; gap: 1rem; padding: 1.1rem; min-width: 0; }
  p, h3, h4, dl { margin: 0; }
  .muted { color: var(--text-muted); font-size: .88rem; }
  .hint { font-size: .76rem; }
  .section-label, .hero-label, dt { font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .section-label { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; margin-bottom: .45rem; color: var(--action-primary); }
  .section-label .aside { margin-left: auto; text-transform: none; letter-spacing: 0; font-weight: 500; color: var(--text-muted); }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }

  .hero { --fill: var(--bg-secondary); --edge: var(--muted-dot); --line: var(--border-subtle); --ink: var(--text-muted); }
  .ready, .students { --fill: var(--ok-fill); --edge: var(--ok); --line: var(--ok-border); --ink: var(--ok-text); }
  .active, .builder { --fill: var(--hover-fill); --edge: var(--action-primary); --line: var(--cyan-border); --ink: var(--action-hover); }
  .hero.failed { --fill: var(--alert-fill); --edge: var(--alert); --line: var(--alert-border); --ink: var(--alert-text); }
  .hero { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; padding: .85rem 1rem; border: 1px solid var(--line); border-left: 5px solid var(--edge); border-radius: 12px; background: var(--fill); color: var(--ink); }
  .badge, .network-icon { display: grid; place-items: center; flex: none; background: var(--edge); color: var(--on-dark); }
  .badge { width: 40px; height: 40px; border-radius: 50%; }
  .hero.neutral .badge { color: var(--text-main); }
  .hero-text { flex: 1 1 14rem; min-width: 0; font-size: .84rem; }
  .hero h3 { color: var(--text-main); font-size: 1.05rem; }
  .chips { display: flex; align-items: center; flex-wrap: wrap; gap: .35rem; }
  .chip { display: inline-flex; align-items: center; gap: .3rem; padding: .1rem .55rem; border-radius: 999px; border: 1px solid var(--border-subtle); background: var(--bg-raised); color: var(--text-muted); font: 700 .75rem var(--font-mono); }
  .spin { display: inline-block; flex: none; width: 18px; height: 18px; border: 2px solid var(--cyan-chip-border); border-top-color: currentColor; border-radius: 50%; animation: spin .8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .stages { display: flex; flex-wrap: wrap; gap: .4rem; list-style: none; margin: 0; padding: 0; }
  .stages li { display: flex; align-items: center; gap: .4rem; flex: 1 1 7rem; padding: .4rem .55rem; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; font-weight: 600; }
  .stage-mark { display: grid; place-items: center; flex: none; width: 18px; height: 18px; border: 2px solid currentColor; border-radius: 50%; }
  .stages li.current { background: var(--hover-fill); border-color: var(--cyan-border); color: var(--action-hover); }
  .current .stage-mark { border-color: var(--cyan-border); border-top-color: var(--action-primary); animation: spin .8s linear infinite; }
  .stages li.done { color: var(--ok-text); }
  .done .stage-mark { background: var(--ok-fill); border-color: var(--ok); color: var(--ok-text); }
  .stages li.failed, .checks li.failed { background: var(--alert-fill); border-color: var(--alert-border); color: var(--alert-text); }
  .progress { display: grid; gap: .35rem; margin-top: .6rem; }
  .progress-line { display: flex; flex-wrap: wrap; align-items: baseline; gap: .2rem .7rem; font-size: .82rem; }
  .progress-line code { min-width: 0; overflow-wrap: anywhere; color: var(--text-muted); }
  .quiet { margin-left: auto; color: var(--text-muted); }
  .bar { height: 7px; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .bar span { display: block; height: 100%; border-radius: inherit; background: var(--action-primary); transition: width .4s ease; }

  .usage { margin-bottom: .6rem; overflow-wrap: anywhere; }
  .network { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .network-card { display: flex; align-items: flex-start; gap: .6rem; padding: .6rem .75rem; border: 1px solid var(--line); border-radius: 10px; background: var(--fill); color: var(--ink); font-size: .84rem; }
  .network-icon { width: 28px; height: 28px; border-radius: 8px; }
  .network-card p { font-size: .78rem; }

  .facts { overflow: hidden; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); }
  .fact { display: grid; grid-template-columns: 7.5rem minmax(0, 1fr); align-items: center; gap: .75rem; padding: .6rem .85rem; }
  .fact + .fact { border-top: 1px solid var(--border-subtle); }
  dt { color: var(--text-muted); }
  dd { display: flex; justify-content: space-between; align-items: center; gap: .6rem; min-width: 0; margin: 0; }
  dd > div { min-width: 0; overflow-wrap: anywhere; }
  code { font-family: var(--font-mono); }
  .sub { display: block; overflow: hidden; color: var(--text-muted); font-size: .76rem; text-overflow: ellipsis; white-space: nowrap; }
  .tag { color: var(--action-hover); font-weight: 700; }
  .small-button { display: inline-flex; align-items: center; gap: .3rem; flex: none; min-height: 28px; margin: 0; padding: 0 .55rem; border-radius: 7px; color: var(--text-muted); background: var(--bg-raised); font-size: .75rem; font-weight: 600; }

  .failure, .warnings, .verified { padding: .55rem .75rem; border: 1px solid; border-radius: 9px; overflow-wrap: anywhere; font-size: .88rem; }
  .failure { border-color: var(--alert-border); background: var(--alert-fill); color: var(--alert-text); }
  .failure .hint { margin-top: .3rem; }
  .warnings { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .warnings ul { margin: .3rem 0 .3rem 1.1rem; padding: 0; }
  .verified { display: flex; align-items: flex-start; gap: .5rem; border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .verified :global(svg), .checks :global(svg) { flex: none; }
  .checks { display: grid; grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr)); gap: .35rem; list-style: none; margin: 0; padding: 0; }
  .checks li { display: flex; align-items: center; gap: .4rem; padding: .3rem .55rem; border: 1px solid var(--ok-border); border-radius: 8px; background: var(--ok-fill); color: var(--ok-text); font: .78rem var(--font-mono); overflow-wrap: anywhere; }

  .log-label { display: flex; align-items: baseline; justify-content: space-between; flex-wrap: wrap; gap: .25rem .65rem; }
  .pick { display: flex; align-items: center; gap: .45rem; max-width: 100%; margin-bottom: .45rem; color: var(--text-muted); font-size: .82rem; }
  .pick select { min-width: 0; margin: 0; padding-block: .25rem; }
  .terminal { overflow: hidden; border: 1px solid var(--term-head); border-radius: 12px; background: var(--term-ground); color: var(--term-text); }
  .log-head { display: flex; align-items: center; flex-wrap: wrap; gap: .4rem .5rem; padding: .45rem .65rem .45rem .85rem; background: var(--term-head); color: var(--term-text); font-size: .8rem; }
  .log-head h4 { color: var(--on-dark); font-size: inherit; }
  .lines { color: var(--term-dim); }
  .log-actions { display: flex; gap: .4rem; margin-left: auto; }
  .log-head .small-button { background: var(--transparent); border-color: var(--cyan-chip-border); color: var(--term-text); }
  .log-head .small-button:hover:not(:disabled) { background: var(--chip-on-dark); border-color: var(--accent-cyan-soft); color: var(--on-dark); }
  .log, .log-note { padding: .6rem .85rem; font-size: .8rem; }
  .log { max-height: 14rem; margin: 0; overflow: auto; border: 0; border-radius: 0; font: .78rem/1.55 var(--font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
  .log:focus-visible { outline-offset: -3px; }
  .step-line { color: var(--term-cyan); font-weight: 700; }
  .ok-line { color: var(--term-bright-green); }
  .fail-line { color: var(--term-bright-red); font-weight: 700; }
  .dim-line { color: var(--term-dim); }
  .follow { display: flex; align-items: center; gap: .4rem; padding: .3rem .85rem; border-top: 1px solid var(--cyan-chip-border); font-size: .8rem; }

  .actions { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: .5rem 1rem; }
  .secondary-actions, .action-row { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; }
  .action-row { justify-content: flex-end; column-gap: .85rem; }
  .build-actions { margin-left: auto; text-align: right; }
  .actions button { display: inline-flex; align-items: center; gap: .45rem; min-height: 38px; margin: 0; padding: .45rem .9rem; border-radius: 9px; }
  .ghost { background: var(--transparent); color: var(--text-muted); }
  .scratch-option { display: inline-flex; align-items: center; gap: .4rem; min-height: 38px; font-size: .88rem; cursor: pointer; }
  .scratch-option:has(input:disabled) { color: var(--text-muted); cursor: not-allowed; }
  .actions .spin { width: 14px; height: 14px; border-top-color: var(--action-primary); }
  .actions .hint, .copy-feedback { color: var(--text-muted); font-size: .76rem; }
  .actions .hint { margin-top: .35rem; }
  .copy-feedback { margin-top: .65rem; }

  @media (max-width: 620px) {
    .network { grid-template-columns: 1fr; }
    .fact { grid-template-columns: minmax(0, 1fr); gap: .25rem; }
    .action-row { justify-content: flex-start; }
    .build-actions { margin-left: 0; text-align: left; }
  }
  @media (prefers-reduced-motion: reduce) {
    .spin, .current .stage-mark { animation: none; }
    .bar span { transition: none; }
  }
</style>
