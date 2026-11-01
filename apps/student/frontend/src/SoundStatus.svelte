<script>
  import Card from '../../../../shared/theme/Card.svelte';
  // sound: the level the computer reports right now (Go checks every few seconds); fields are absent when unreadable.
  let { sound = {} } = $props();
  let label = $derived([sound.volume_percent == null ? '' : `${sound.volume_percent}%`, sound.volume_muted ? 'muted' : ''].filter(Boolean).join(' · ') || 'not available');
</script>

<Card label="Sound volume" title="Sound volume" icon="volume" level={3} tone="info">
  <p class="level" aria-label="Current sound volume">Current volume: <strong>{label}</strong>{#if sound.volume_percent != null}<span class="volume-bar" class:muted={sound.volume_muted} aria-hidden="true"><span style:width="{Math.min(100, Math.max(0, sound.volume_percent))}%"></span></span>{/if}</p>
  <p>The alarm must be heard: after you log in, the app turns the volume up if needed and restores it when the exam ends.</p>
</Card>

<style>
  p { margin: 0 0 var(--space-2); overflow-wrap: anywhere; }
  p:last-child { margin-bottom: 0; }
  /* The level reads "Current volume [bar] 35%": the bar is drawn between the label and the number, and is decorative. */
  .level { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem .8rem; font-size: var(--text-base); }
  .level strong { order: 2; font-family: var(--font-mono); color: var(--action-hover); }
  .volume-bar { order: 1; flex: 1 1 8rem; height: 8px; background: var(--bg-secondary); border-radius: 999px; overflow: hidden; }
  .volume-bar span { display: block; height: 100%; background: var(--bar-fill); border-radius: inherit; }
  .volume-bar.muted span { background: var(--muted-dot); }
</style>
