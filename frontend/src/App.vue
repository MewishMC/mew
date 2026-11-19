<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import Sidebar from './components/layout/Sidebar.vue'
import HomePage from './components/pages/HomePage.vue'
import PackPorter from './components/tools/PackPorter.vue'
import FolderPorter from './components/tools/FolderPorter.vue'
import RecolorTool from './components/tools/RecolorTool.vue'
import AnimatedInventory from './components/tools/AnimatedInventory.vue'
import RecolorEditor from './components/tools/RecolorEditor.vue'
import InfoPage from './components/pages/InfoPage.vue'
import AppearancePage from './components/pages/AppearancePage.vue'
import SettingsPage from './components/pages/SettingsPage.vue'
import ContactPage from './components/pages/ContactPage.vue'
import RecentPacksPage from './components/pages/RecentPacksPage.vue'
import SkyConverter from './components/tools/SkyConverter.vue'
import PackViewer from './components/tools/PackViewer.vue'
import ManifestTool from './components/tools/ManifestTool.vue'
import ProgressNotification from './components/shared/ProgressNotification.vue'
import { GetSettings, SaveSettings, CheckForUpdate, OpenDownloadLink, SetDiscordActivity } from '../wailsjs/go/main/App'
import { WindowMinimise, WindowToggleMaximise, WindowIsMaximised, Quit } from '../wailsjs/runtime/runtime'
import { updaterState, startUpdate } from './utils/updater'
import { applyTheme } from './utils/appearance'

const currentPage = ref('home')

const pageTitles = {
  home: 'Home',
  packporter: 'Pack Porter',
  packFolderPorter: 'Multi-Pack Porter',
  recolor: 'Pack Editor',
  recoloreditor: 'Pack Editor',
  animator: 'Animated Inventory',
  skyconverter: 'Sky Converter',
  packviewer: 'Pack Viewer',
  manifest: 'Manifest',
  recentpacks: 'Recent Packs',
  info: 'Info',
  appearance: 'Appearance',
  settings: 'Settings',
  contact: 'Contact',
}

const pageTitle = computed(() => pageTitles[currentPage.value] || 'Home')
const recolorSource = ref(null)
const packViewRequest = ref(null)
const sidebarWidth = ref(232)

const homeAnimated = ref(localStorage.getItem('mew_home_animated') === '1')
const updateAvailable = ref(false)
const latestVersion = ref('')
const updateDismissed = ref(false)
const isMaximised = ref(false)

// Wails injects window.runtime; absent in the browser preview, so guard it.
function hasRuntime() {
  return typeof window !== 'undefined' && !!window.runtime
}

async function syncMaximised() {
  if (!hasRuntime()) return
  try {
    isMaximised.value = await WindowIsMaximised()
  } catch (e) {
    // keep last known state
  }
}

function minimiseWindow() {
  if (hasRuntime()) WindowMinimise()
}

function toggleMaximise() {
  if (!hasRuntime()) return
  WindowToggleMaximise()
  setTimeout(syncMaximised, 120)
}

function closeWindow() {
  if (hasRuntime()) Quit()
}

function onHomeAnimated() {
  homeAnimated.value = true
  localStorage.setItem('mew_home_animated', '1')
}

function switchPage(page) {
  currentPage.value = page
}

function onSidebarWidthChange(w) {
  sidebarWidth.value = w
}

function openDisplay(data) {
  recolorSource.value = data
  currentPage.value = 'recoloreditor'
}

function closeDisplay() {
  const data = recolorSource.value
  const target = data && data.dirName
  recolorSource.value = null
  if (target && data.kind === 'installed') {
    packViewRequest.value = { name: target, ts: Date.now() }
    currentPage.value = 'packviewer'
  } else {
    currentPage.value = 'recolor'
  }
}

function dismissUpdate() {
  if (updaterState.busy) return
  updateDismissed.value = true
}

async function syncSettingsToBackend() {
  try {
    const s = await GetSettings()
    await SaveSettings(s)
    applyTheme(s.theme || 'dark')
  } catch (e) {
    console.error('Failed to sync settings:', e)
  }
}

async function checkForUpdates() {
  try {
    const info = await CheckForUpdate()
    if (info.needsUpdate) {
      updateAvailable.value = true
      latestVersion.value = info.latestVersion
    }
  } catch (e) {
    console.error('Update check failed:', e)
  }
}

onMounted(() => {
  syncMaximised()
  syncSettingsToBackend()
  checkForUpdates()
  SetDiscordActivity('Browsing MEW', 'Minecraft Bedrock Texture Pack Manager')
  window.addEventListener('wheel', blockCtrlZoom, { passive: false })
})

onUnmounted(() => {
  window.removeEventListener('wheel', blockCtrlZoom)
})

function blockCtrlZoom(e) {
  if (e.ctrlKey || e.metaKey) e.preventDefault()
}
</script>

<template>
  <div class="app-layout">
    <div class="page-bar" style="--wails-draggable: drag" @dblclick="toggleMaximise">
      <span class="pb-ball" aria-hidden="true"></span>
      <span class="pb-title">{{ pageTitle }}</span>

      <div class="pb-controls" style="--wails-draggable: no-drag" @dblclick.stop>
        <button class="pb-btn" title="Minimise" @click="minimiseWindow">
          <i class="fa fa-minus"></i>
        </button>
        <button class="pb-btn" :title="isMaximised ? 'Restore' : 'Maximise'" @click="toggleMaximise">
          <i class="fa" :class="isMaximised ? 'fa-clone' : 'fa-expand'"></i>
        </button>
        <button class="pb-btn pb-btn-close" title="Close" @click="closeWindow">
          <i class="fa fa-xmark"></i>
        </button>
      </div>
    </div>

    <div v-if="updateAvailable && !updateDismissed" class="update-banner">
      <i class="fa fa-download update-icon"></i>

      <template v-if="updaterState.state === 'restarting'">
        <span class="update-text">Installing v{{ latestVersion }} &mdash; MEW will restart</span>
      </template>

      <template v-else-if="updaterState.state === 'error'">
        <span class="update-text">Update failed &mdash; {{ updaterState.error }}</span>
        <button class="update-btn" @click="startUpdate"><i class="fa fa-rotate-right"></i> Retry</button>
        <button class="update-btn" @click="OpenDownloadLink('https://github.com/KyarottoOwO/mew/releases')">View on GitHub</button>
      </template>

      <template v-else>
        <span class="update-text">A new version (<strong>v{{ latestVersion }}</strong>) is available!</span>
        <button class="update-btn" :disabled="updaterState.busy" @click="startUpdate"><i class="fa fa-arrow-up"></i> Update now</button>
      </template>

      <button class="update-dismiss" @click="dismissUpdate"><i class="fa fa-xmark"></i></button>
    </div>

    <div class="app-main">
      <Sidebar :current-page="currentPage" @navigate="switchPage" @width-change="onSidebarWidthChange" />
      <HomePage v-if="currentPage === 'home'" :home-animated="homeAnimated" @navigate="switchPage" @animated="onHomeAnimated" />
      <PackPorter v-show="currentPage === 'packporter'" :active="currentPage === 'packporter'" />
      <FolderPorter v-show="currentPage === 'packFolderPorter'" :active="currentPage === 'packFolderPorter'" />
      <RecolorTool v-show="currentPage === 'recolor'" :active="currentPage === 'recolor'" @open-display="openDisplay" />
      <AnimatedInventory v-show="currentPage === 'animator'" :active="currentPage === 'animator'" />
      <template v-if="recolorSource">
        <RecolorEditor v-show="currentPage === 'recoloreditor'" :visible="currentPage === 'recoloreditor'" :source="recolorSource" :sidebar-width="sidebarWidth" @close="closeDisplay" />
      </template>
      <RecentPacksPage v-if="currentPage === 'recentpacks'" />
      <SkyConverter v-show="currentPage === 'skyconverter'" :active="currentPage === 'skyconverter'" />
      <PackViewer v-show="currentPage === 'packviewer'" :active="currentPage === 'packviewer'" :open-pack-req="packViewRequest" />
      <ManifestTool v-show="currentPage === 'manifest'" :active="currentPage === 'manifest'" />
      <InfoPage v-show="currentPage === 'info'" />
      <AppearancePage v-show="currentPage === 'appearance'" />
      <SettingsPage v-show="currentPage === 'settings'" />
      <ContactPage v-show="currentPage === 'contact'" />
    </div>

    <ProgressNotification :current-page="currentPage" @navigate="switchPage" />
  </div>
</template>

<style>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

body {
  background-color: var(--bg-body);
  color: var(--text-primary);
  font-family: var(--font-body);
  overflow: hidden;
}

.page-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  flex-shrink: 0;
  padding: 0 8px 0 4px;
  background: var(--accent);
  border-bottom: 2px solid var(--accent-dark);
  z-index: 25;
}

/* Poke Ball: the sprite lifted 1:1 from the reference artwork. The light
   badge is used for the light theme, a dark badge for the dark theme. */
.pb-ball {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  background: url('/pokeball.png') center / 28px 28px no-repeat;
  image-rendering: pixelated;
  /* Follows the accent hue picked in Settings > Appearance. */
  filter: hue-rotate(var(--ball-hue, 0deg));
}

:root[data-theme="dark"] .pb-ball {
  background-image: url('/pokeball-dark.png');
}

.pb-title {
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 14px;
  border-right: 2px solid var(--accent-dark);
  font-family: var(--font-pixel);
  font-weight: 700;
  font-size: 0.78rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #fff;
  line-height: 1;
  white-space: nowrap;
}

.pb-controls {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
  height: 100%;
  padding-left: 8px;
  border-left: 2px solid var(--accent-dark);
}

.pb-btn {
  width: 28px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  background: transparent;
  border: 2px solid transparent;
  border-radius: 4px !important;
  color: #fff;
  font-size: 11px;
  line-height: 1;
  cursor: pointer;
  box-shadow: none !important;
}

.pb-btn:hover {
  background: rgba(0, 0, 0, 0.18);
  border-color: var(--accent-dark);
}

.pb-btn:active {
  transform: translateY(1px);
}

.pb-btn-close:hover {
  background: var(--accent-dark);
  border-color: var(--accent-dark);
}

.app-layout {
  display: flex;
  flex-direction: column;
  height: 100vh;
  width: 100vw;
}

.app-main {
  display: flex;
  flex: 1;
  min-height: 0;
}

.app-main > :not(.sidebar) {
  flex: 1;
  min-width: 0;
  min-height: 0;
}

.page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
}

.update-banner {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  padding: 0.5rem 1rem;
  background: var(--accent-glow2);
  border-bottom: 1px solid var(--accent-border);
  color: var(--text-secondary);
  font-size: 0.8rem;
  flex-shrink: 0;
  z-index: 20;
}

.update-icon {
  color: var(--accent);
}

.update-banner strong {
  color: var(--accent);
}

.update-btn {
  margin-left: auto;
  padding: 0.25rem 0.75rem;
  background: transparent;
  color: var(--accent);
  border: 1px solid var(--accent);
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.75rem;
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex-shrink: 0;
}

.update-btn:hover {
  background: var(--accent-glow);
}

.update-btn + .update-btn {
  margin-left: 0;
}

.update-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.update-dismiss {
  background: none;
  border: none;
  color: var(--text-dim);
  cursor: pointer;
  font-size: 0.85rem;
  padding: 0.2rem;
}

.update-dismiss:hover {
  color: var(--text-secondary);
}
</style>
