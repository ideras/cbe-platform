<script>
  import Card from '../../../../shared/theme/Card.svelte';
  // sound: the level the computer reports right now (Go checks every few seconds); fields are absent when unreadable.
  let { sound = {} } = $props();
  let label = $derived([sound.volume_percent == null ? '' : `${sound.volume_percent}%`, sound.volume_muted ? 'muted' : ''].filter(Boolean).join(' · ') || 'not available');
</script>

<Card label="Sound volume" title="Sound volume" level={3} tone="info">
  <p class="level" aria-label="Current sound volume">Current volume: <strong>{label}</strong></p>
  <p>The alarm must be heard: after you log in, the app turns the volume up if needed and restores it when the exam ends.</p>
</Card>

<style>
  p { margin: 0 0 var(--space-2); overflow-wrap: anywhere; }
  p:last-child { margin-bottom: 0; }
  .level { font-size: var(--text-base); }
</style>
