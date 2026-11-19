<script setup>
import { ref, computed, onMounted } from 'vue'
import { GetVersion } from '../../../wailsjs/go/main/App'

defineProps({
  currentPage: String
})

const emit = defineEmits(['navigate', 'width-change'])

const version = ref('')

onMounted(async () => {
  try {
    version.value = await GetVersion()
  } catch (e) {
    // No Wails runtime (browser preview).
  }
})

const navGroups = [
  {
    title: 'Dashboard',
    items: [
      { id: 'home', icon: 'fa-house', label: 'Home' },
      { id: 'recentpacks', icon: 'fa-clock', label: 'Recent Packs' },
    ],
  },
  {
    title: 'Tools',
    items: [
      { id: 'packporter', icon: 'fa-box-open', label: 'Pack Porter' },
      { id: 'packFolderPorter', icon: 'fa-folder-open', label: 'Multi-Pack Porter' },
      { id: 'recolor', icon: 'fa-palette', label: 'Pack Editor' },
      { id: 'animator', icon: 'fa-video', label: 'Animated Inventory' },
      { id: 'skyconverter', icon: 'fa-cloud-sun', label: 'Sky Converter' },
      { id: 'packviewer', icon: 'fa-eye', label: 'Pack Viewer' },
      { id: 'manifest', icon: 'fa-file-lines', label: 'Manifest' },
    ],
  },
  {
    title: 'App',
    items: [
      { id: 'about', icon: 'fa-circle-info', label: 'About' },
      { id: 'changelog', icon: 'fa-clipboard-list', label: 'Changelog' },
      { id: 'appearance', icon: 'fa-swatchbook', label: 'Appearance' },
      { id: 'settings', icon: 'fa-gear', label: 'Settings' },
      { id: 'contact', icon: 'fa-envelope', label: 'Contact' },
    ],
  },
]

const EXPANDED_WIDTH = 232

const isExpanded = ref(true)
const sidebarWidth = computed(() => EXPANDED_WIDTH)

const tooltipVisible = ref(false)
const tooltipText = ref('')
const tooltipStyle = ref({})

function showTooltip(e, text) {
  if (isExpanded.value) return
  tooltipText.value = text
  const rect = e.currentTarget.getBoundingClientRect()
  tooltipStyle.value = {
    left: (rect.right + 10) + 'px',
    top: (rect.top + rect.height / 2) + 'px',
  }
  tooltipVisible.value = true
}

function hideTooltip() {
  tooltipVisible.value = false
}
</script>

<template>
  <aside
    class="sidebar"
    :class="{ expanded: isExpanded }"
    :style="{ width: sidebarWidth + 'px', minWidth: sidebarWidth + 'px' }"
  >
    <div class="rail-logo">
      <div class="logo-frame">
        <img src="/logo-small.png" alt="MEW" />
      </div>
      <div class="brand-text">
        <h1>MEW</h1>
        <span>Bedrock pack tools</span>
      </div>
    </div>

    <nav id="nav">
      <div class="nav-group" v-for="group in navGroups" :key="group.title">
        <span class="nav-title" v-if="isExpanded">{{ group.title }}</span>
        <button
          v-for="item in group.items"
          :key="item.id"
          class="nav-item"
          :class="{ active: currentPage === item.id }"
          @click="emit('navigate', item.id)"
          @mouseenter="showTooltip($event, item.label)"
          @mouseleave="hideTooltip"
        >
          <i :class="'fa ' + item.icon" aria-hidden="true"></i>
          <span class="nav-label" v-if="isExpanded">{{ item.label }}</span>
        </button>
      </div>
    </nav>

    <div class="sidebar-foot">
      <span class="version">v{{ version || '—' }}</span>
    </div>

    <div class="tooltip" v-if="tooltipVisible" :style="tooltipStyle">{{ tooltipText }}</div>
  </aside>
</template>

<style scoped>
.sidebar {
  position: relative;
  margin: 10px 0 10px 10px;
  background: var(--bg-sidebar);
  border: 1px solid var(--border-default);
  border-radius: 16px;
  display: flex;
  flex-direction: column;
  padding: 14px 12px 12px;
  overflow: hidden;
  flex-shrink: 0;
  z-index: 10;
}

.rail-logo {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 4px 14px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--border-subtle);
}

.logo-frame {
  width: 38px;
  height: 38px;
  flex: 0 0 auto;
  padding: 2px;
  border-radius: 10px;
  border: 1px solid var(--accent-border);
  background: var(--accent-soft);
  overflow: hidden;
}

.logo-frame img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  border-radius: 8px;
  image-rendering: auto;
}

.brand-text {
  min-width: 0;
}

.brand-text h1 {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0.2px;
  color: var(--text-primary);
  line-height: 1.2;
}

.brand-text span {
  font-size: 9.5px;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  color: var(--text-dim);
}

#nav {
  width: 100%;
  padding: 0 2px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.nav-group {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin-bottom: 10px;
}

.nav-title {
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.9px;
  text-transform: uppercase;
  color: var(--text-faint);
  padding: 2px 10px 4px;
}

.nav-item {
  position: relative;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  padding: 0 10px;
  border: 1px solid transparent !important;
  border-radius: 8px !important;
  background: transparent !important;
  color: var(--text-muted);
  font-family: var(--font-body) !important;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.sidebar:not(.expanded) .nav-item {
  justify-content: center;
  padding: 0;
}

.nav-item i {
  font-size: 16px;
  width: 22px;
  text-align: center;
  flex-shrink: 0;
  line-height: 1;
}

.nav-item:hover {
  background: var(--bg-hover-1) !important;
  color: var(--text-secondary);
}

.nav-item.active {
  background: var(--accent-soft) !important;
  color: var(--accent-light);
  font-weight: 600;
}

.nav-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: var(--accent);
}

.nav-item.active i {
  color: var(--accent);
}

.nav-label {
  font-size: 12.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sidebar-foot {
  margin-top: auto;
  width: 100%;
  padding: 9px 2px 0;
  border-top: 1px solid var(--border-subtle);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
  font-size: 10.5px;
  color: var(--text-dim);
}

.version {
  font-size: 10px;
  letter-spacing: 0.3px;
  color: var(--text-version);
  padding-left: 2px;
  user-select: none;
}

.sidebar:not(.expanded) .version {
  text-align: center;
  padding-left: 0;
}

.tooltip {
  position: fixed;
  transform: translateY(-50%);
  background: var(--bg-surface);
  border: 1px solid var(--border-medium);
  color: var(--text-secondary);
  padding: 5px 10px;
  border-radius: 6px;
  font-size: 12px;
  white-space: nowrap;
  z-index: 100;
  pointer-events: none;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
}
</style>
