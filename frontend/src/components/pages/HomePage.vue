<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { GetRecentPacks } from '../../../wailsjs/go/main/App'
import { EventsOn } from '../../../wailsjs/runtime/runtime'

import { parseBedrockCodes } from '../../utils/formatCodes'
const props = defineProps({ homeAnimated: Boolean })
const emit = defineEmits(['navigate', 'animated'])
const showSuite = ref(false)
const hasAnimated = ref(false)
const isMounted = ref(false)
const recentPacks = ref([])
let offRecentPacks = null

async function loadRecentPacks() {
  try {
    const packs = await GetRecentPacks()
    if (isMounted.value) recentPacks.value = packs || []
  } catch (e) {
    console.error('Failed to load recent packs:', e)
  }
}

onMounted(() => {
  isMounted.value = true
  if (props.homeAnimated) {
    showSuite.value = true
    hasAnimated.value = true
  } else {
    emit('animated')
    setTimeout(() => { showSuite.value = true }, 2500)
    setTimeout(() => { hasAnimated.value = true }, 3500)
  }
  loadRecentPacks()
  // The home page is mounted with v-if, so every visit mounts it again; the
  // handler has to be dropped on unmount or the listeners pile up.
  offRecentPacks = EventsOn('recent-packs-changed', () => {
    if (isMounted.value) loadRecentPacks()
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
  <div class="home-page">
    <h1 v-if="!showSuite" class="welcome-text" :class="{ 'fade-in': !hasAnimated }">Welcome to MEW</h1>

    <div v-if="showSuite" class="suite-content" :class="{ 'fade-in': !hasAnimated }">
      <h1 class="suite-title">Pack Tools Suite</h1>
      <p class="suite-sub">All the tools you need to handle Minecraft Bedrock texture packs in one place.</p>

      <div class="flex gap-8 justify-center mb-10 home-cards">
        <div class="home-card" @click="emit('navigate', 'packporter')">
          <div class="home-card-header">Pack Porter</div>
          <h3 class="home-card-text">Turn Java Edition resource packs into Bedrock Edition fast and easy.</h3>
        </div>
        <div class="home-card" @click="emit('navigate', 'recolor')">
          <div class="home-card-header">Pack Editor</div>
          <h3 class="home-card-text">Edit and recolor texture packs to your liking</h3>
        </div>
      </div>

      <div class="about-box">
        <h3 class="box-header">About MEW</h3>
        <p class="text-sm mb-4" style="color: var(--text-desc);">MEW helps you work with resource packs faster by automating common tasks and keeping things simple.</p>
        <div class="flex justify-around">
          <div>
            <span class="font-bold" style="color: var(--accent);">Fast</span>
            <p class="text-sm" style="color: var(--text-desc);">Quick and smooth performance.</p>
          </div>
          <div>
            <span class="font-bold" style="color: var(--accent);">User-Friendly</span>
            <p class="text-sm" style="color: var(--text-desc);">Simple and accessible for all.</p>
          </div>
        </div>
      </div>

      <div v-if="recentPacks.length > 0" class="recent-box">
        <h3 class="box-header">Recent Packs</h3>
        <div class="recent-list">
          <div v-for="(pack, i) in recentPacks" :key="i" class="recent-item">
            <div class="recent-info">
              <i class="fa fa-box-open recent-icon"></i>
              <span class="recent-name" v-html="parseBedrockCodes(pack.name)"></span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.home-page {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: safe center;
  padding: 2rem;
  overflow-y: auto;
}

.welcome-text {
  font-size: 2.25rem;
  font-weight: bold;
  color: var(--accent);
  animation: fade-in 1s ease-in;
}

.suite-content {
  text-align: center;
  transition: opacity 0.7s ease;
}

.suite-title {
  font-family: var(--font-pixel);
  font-size: 2rem;
  font-weight: 700;
  color: var(--accent);
  margin-bottom: 0.5rem;
}

.suite-sub {
  font-family: var(--font-body);
  font-size: 0.95rem;
  color: var(--text-muted);
  margin-bottom: 2rem;
}

.home-cards {
  flex-wrap: wrap;
}

.home-card {
  padding: 1.5rem;
  padding-top: 0;
  width: 280px;
  max-width: 100%;
  cursor: pointer;
  overflow: hidden;
  text-align: left;
  transition: transform 0.1s ease, box-shadow 0.1s ease;
}

.home-card:hover {
  transform: translateY(-2px);
}

.home-card-header {
  margin: 0 -1.5rem 0.9rem;
  padding: 0.55rem 0.9rem;
  background: var(--accent);
  border-bottom: 2px solid var(--accent-dark);
  color: #fff;
  font-family: var(--font-pixel);
  font-size: 0.95rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.home-card-text {
  font-family: var(--font-body);
  font-size: 0.875rem;
  font-weight: 400;
  color: var(--text-muted);
  line-height: 1.5;
}

.about-box {
  padding: 0 1.5rem 1.5rem;
  max-width: 600px;
  margin: 0 auto;
  overflow: hidden;
}

.box-header {
  margin: 0 -1.5rem 1rem;
  padding: 0.55rem 0.9rem;
  background: var(--accent);
  border-bottom: 2px solid var(--accent-dark);
  color: #fff;
  font-family: var(--font-pixel);
  font-size: 0.95rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  text-align: left;
}

.recent-box {
  padding: 0 1.5rem 1rem;
  max-width: 600px;
  margin: 1.5rem auto 0;
  text-align: left;
  overflow: hidden;
}

.recent-title {
  font-family: var(--font-pixel);
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--accent);
  margin-bottom: 0.75rem;
}

.recent-list {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}

.recent-item {
  display: flex;
  align-items: center;
  padding: 0.45rem 0.5rem;
  border-bottom: 2px dotted var(--border-default);
}

.recent-item:last-child {
  border-bottom: none;
}

.recent-info {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}

.recent-icon {
  color: var(--accent);
  font-size: 0.75rem;
  flex-shrink: 0;
}

.recent-name {
  font-size: 0.8rem;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: pre-line;
}

.fade-in {
  animation: fadeIn 1s ease-in forwards;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}
</style>
