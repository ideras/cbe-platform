<script>
  import { studentHighlights } from './student-roster.js';
  let { text, query } = $props();
  const parts = $derived(studentHighlights(text, query));
</script>
{#each parts as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
<style>mark { padding: 0 1px; border-radius: 3px; background: var(--warn-fill); color: var(--warn-text); }</style>
