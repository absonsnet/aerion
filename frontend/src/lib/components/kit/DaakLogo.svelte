<!-- Daak envelope mark in a mint-to-emerald vertical gradient (#A7F3D0 top
     fading to #047857 bottom; inline vector of the approved mark).
     Shared by the title bar and the About page so both render the same logo. -->
<script module lang="ts">
  let instanceCounter = 0
</script>

<script lang="ts">
  interface Props {
    class?: string
    label?: string
  }

  let { class: className = '', label = 'Daak Logo' }: Props = $props()

  // Unique mask and gradient ids per instance — the title bar and the
  // About page can be mounted at the same time, and duplicate SVG ids
  // would collide.
  const instanceId = ++instanceCounter
  const maskId = `daak-flap-${instanceId}`
  const gradientId = `daak-gradient-${instanceId}`
</script>

<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" class={className} role="img" aria-label={label}>
  <defs>
    <linearGradient id={gradientId} gradientUnits="userSpaceOnUse" x1="256" y1="67" x2="256" y2="445">
      <stop offset="0" stop-color="#A7F3D0" />
      <stop offset="1" stop-color="#047857" />
    </linearGradient>
    <mask id={maskId}>
      <rect width="512" height="512" fill="#FFFFFF" />
      <polyline points="26,149 256,315 486,149" fill="none" stroke="#000000" stroke-width="41" stroke-linejoin="round" />
    </mask>
  </defs>
  <rect x="26" y="67" width="461" height="378" rx="68" ry="75" fill="url(#{gradientId})" mask="url(#{maskId})" />
</svg>
