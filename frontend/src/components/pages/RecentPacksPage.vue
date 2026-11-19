<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { GetAllRecentPacks } from '../../../wailsjs/go/main/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { parseBedrockCodes } from '../../utils/formatCodes'
const recentPacks = ref([])
const isMounted = ref(false)
let offRecentPacks = null

async function loadPacks() {
  try {
    const packs = await GetAllRecentPacks()
    if (isMounted.value) recentPacks.value = packs || []
  } catch (e) {
    console.error('Failed to load recent packs:', e)
  }
}

function formatTimestamp(ts) {
  if (!ts) return ''
  const d = new Date(ts.replace(' ', 'T'))
  const now = new Date()
  const diff = now - d
  const dayMs = 86400000

  const options = { hour: 'numeric', minute: '2-digit', hour12: true }
  const timeStr = d.toLocaleTimeString('en-US', options)

  if (diff < dayMs && d.getDate() === now.getDate()) {
    return `Today ${timeStr}`
  }
  const yesterday = new Date(now)
  yesterday.setDate(yesterday.getDate() - 1)
  if (d.getDate() === yesterday.getDate() && d.getMonth() === yesterday.getMonth() && d.getFullYear() === yesterday.getFullYear()) {
    return `Yesterday ${timeStr}`
  }
  if (diff < 7 * dayMs) {
    const dayName = d.toLocaleDateString('en-US', { weekday: 'short' })
    return `${dayName} ${timeStr}`
  }
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

onMounted(() => {
  isMounted.value = true
  loadPacks()
  // Mounted with v-if, so the handler must be dropped again on unmount.
  offRecentPacks = EventsOn('recent-packs-changed', () => {
    if (isMounted.value) loadPacks()
  })
})

onUnmounted(() => {
  isMounted.value = false
  if (offRecentPacks) {
    offRecentPacks()
    offRecentPacks = null
  }
})
</script>

<template>
  <div class="page active-page recentpacks-page">
    <div class="rp-content">
      <section class="rp-head page-bar-card">
        <h2 class="card-title"><i class="fa fa-clock"></i> Recent Packs</h2>
        <p class="rp-desc">Recently ported packs.</p>

        <div v-if="recentPacks.length === 0" class="rp-empty">
        <i class="fa fa-box-open"></i>
        <p>No packs ported yet.</p>
      </div>

        <div v-else class="rp-list">
          <div v-for="(pack, i) in recentPacks" :key="i" class="rp-entry">
            <div class="rp-header">
              <i class="fa fa-box-open rp-icon"></i>
              <span class="rp-name" v-html="parseBedrockCodes(pack.name)"></span>
              <span class="rp-date">{{ formatTimestamp(pack.timestamp) }}</span>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.recentpacks-page {
  justify-content: flex-start;
  align-items: center;
  padding: 2rem 1rem;
  overflow-y: auto;
}

.rp-content {
  max-width: 840px;
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.rp-desc {
  font-size: 0.875rem;
  color: var(--text-desc);
  margin: 0 0 0.6rem;
}

.rp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 1.5rem 0 0.5rem;
  color: var(--text-dim);
  font-size: 0.85rem;
}

.rp-empty i {
  font-size: 2rem;
  color: var(--text-faint);
}

.rp-list {
  width: 100%;
  display: flex;
  flex-direction: column;
}

/* Flat rows inside the card: the shared panel styling would nest a card in a
   card and spill past the box, so it is reset here. */
.rp-entry {
  background: transparent !important;
  border: 0 !important;
  border-radius: 0 !important;
  box-shadow: none !important;
  padding: 0.55rem 0;
}

.rp-entry + .rp-entry {
  border-top: 1px solid var(--border-subtle) !important;
}

.rp-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.3rem;
}

.rp-icon {
  color: var(--accent);
  font-size: 0.85rem;
  width: 18px;
  text-align: center;
  flex-shrink: 0;
}

.rp-name {
  font-weight: 600;
  font-size: 0.9rem;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: pre-line;
}

.rp-date {
  font-size: 0.75rem;
  color: var(--text-dim);
  margin-left: auto;
  flex-shrink: 0;
}


</style>
