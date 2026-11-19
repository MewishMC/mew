<script setup>
import { ref, computed } from 'vue'
import { GetSettings, SaveSettings } from '../../../wailsjs/go/main/App'
import {
  CUSTOM_FONT_FAMILY,
  accentHue,
  neutralColours,
  sidebarSide,
  glassPanels,
  backdropImage,
  interfaceFont,
  uploadedFont,
  customCss,
  currentTheme,
  applyTheme,
  applyAppearance,
  appearanceSnapshot,
  resetColours,
} from '../../utils/appearance'

const FONT_CHOICES = [
  'Segoe UI',
  'Calibri',
  'Candara',
  'Bahnschrift',
  'Cascadia Code',
  'Consolas',
  'Courier New',
  'Georgia',
  'Palatino Linotype',
  'Trebuchet MS',
  'Verdana',
  'Tahoma',
  'Comic Sans MS',
  'Impact',
  'Arial Black',
]

const backdropInput = ref(null)
const fontInput = ref(null)
const appearanceInput = ref(null)
const fontNotice = ref('')

const theme = currentTheme

const fontChoice = computed({
  get() {
    if (uploadedFont.value) return CUSTOM_FONT_FAMILY
    return interfaceFont.value || ''
  },
  set(value) {
    fontNotice.value = ''
    if (value === CUSTOM_FONT_FAMILY) {
      interfaceFont.value = ''
      return
    }
    interfaceFont.value = value
  },
})

const fontPreview = computed(() => {
  const family = uploadedFont.value
    ? CUSTOM_FONT_FAMILY
    : interfaceFont.value || 'Segoe UI'
  return `"${family}", 'Segoe UI', system-ui, sans-serif`
})

function setTheme(value) {
  applyTheme(value)
  persistTheme(value)
}

async function persistTheme(value) {
  try {
    const settings = await GetSettings()
    settings.theme = value
    await SaveSettings(settings)
  } catch (e) {
    // No Wails runtime (browser preview): the theme still switches live.
  }
}

function pickBackdrop() {
  backdropInput.value?.click()
}

async function onBackdropFile(event) {
  const file = event.target.files && event.target.files[0]
  event.target.value = ''
  if (!file) return
  try {
    backdropImage.value = await shrinkImage(file)
  } catch (e) {
    console.error('Could not read that image:', e)
  }
}

function shrinkImage(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error)
    reader.onload = () => {
      const img = new Image()
      img.onerror = () => reject(new Error('not an image'))
      img.onload = () => {
        const maxW = 1920
        const maxH = 1200
        const scale = Math.min(1, maxW / img.width, maxH / img.height)
        const canvas = document.createElement('canvas')
        canvas.width = Math.max(1, Math.round(img.width * scale))
        canvas.height = Math.max(1, Math.round(img.height * scale))
        const ctx = canvas.getContext('2d')
        ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
        let dataUrl = canvas.toDataURL('image/jpeg', 0.84)
        if (dataUrl.length > 3_500_000) dataUrl = canvas.toDataURL('image/jpeg', 0.6)
        resolve(dataUrl)
      }
      img.src = reader.result
    }
    reader.readAsDataURL(file)
  })
}

function pickFont() {
  fontInput.value?.click()
}

function onFontFile(event) {
  const file = event.target.files && event.target.files[0]
  event.target.value = ''
  if (!file) return
  if (file.size > 3_000_000) {
    fontNotice.value = 'That font is over 3 MB, so it was not loaded.'
    return
  }
  const reader = new FileReader()
  reader.onerror = () => {
    fontNotice.value = 'That font could not be read.'
  }
  reader.onload = () => {
    fontNotice.value = ''
    uploadedFont.value = String(reader.result)
    interfaceFont.value = ''
  }
  reader.readAsDataURL(file)
}

function removeFont() {
  uploadedFont.value = ''
  interfaceFont.value = ''
  fontNotice.value = ''
}

function exportAppearance() {
  const blob = new Blob([JSON.stringify({ version: 1, ...appearanceSnapshot() }, null, 2)], {
    type: 'application/json',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'mew-appearance.json'
  link.click()
  URL.revokeObjectURL(url)
}

function pickAppearanceFile() {
  appearanceInput.value?.click()
}

function onAppearanceFile(event) {
  const file = event.target.files && event.target.files[0]
  event.target.value = ''
  if (!file) return
  const reader = new FileReader()
  reader.onerror = () => {}
  reader.onload = () => {
    try {
      const data = JSON.parse(String(reader.result))
      if (typeof data.hue === 'number' && data.hue >= 0 && data.hue <= 360) {
        accentHue.value = Math.round(data.hue)
      }
      if (typeof data.neutral === 'boolean') neutralColours.value = data.neutral
      if (data.side === 'left' || data.side === 'right') sidebarSide.value = data.side
      if (typeof data.glass === 'boolean') glassPanels.value = data.glass
      if (typeof data.backdrop === 'string') backdropImage.value = data.backdrop
      if (typeof data.font === 'string') interfaceFont.value = data.font
      if (typeof data.fontData === 'string') uploadedFont.value = data.fontData
      if (typeof data.css === 'string') customCss.value = data.css
      applyAppearance()
    } catch (e) {
      console.error('Not an appearance file:', e)
    }
  }
  reader.readAsText(file)
}
</script>

<template>
  <div class="page active-page appearance-page">
    <div class="ap-wrap">
      <header class="ap-head">
        <h2><i class="fa fa-palette"></i> Appearance</h2>
        <p>Sidebar side, colour, font and backdrop &mdash; how MEW looks to you</p>
      </header>

      <section class="ap-card">
        <h3 class="ap-card-title">Layout</h3>
        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Sidebar side</span>
            <span class="ap-desc">Keep the navigation on the left or the right</span>
          </div>
          <div class="seg">
            <button type="button" :class="{ on: sidebarSide === 'left' }" @click="sidebarSide = 'left'">Left</button>
            <button type="button" :class="{ on: sidebarSide === 'right' }" @click="sidebarSide = 'right'">Right</button>
          </div>
        </div>
      </section>

      <section class="ap-card">
        <h3 class="ap-card-title">Colour</h3>

        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Theme</span>
            <span class="ap-desc">Dark or light surfaces</span>
          </div>
          <div class="seg">
            <button type="button" :class="{ on: theme === 'dark' }" @click="setTheme('dark')">Dark</button>
            <button type="button" :class="{ on: theme === 'light' }" @click="setTheme('light')">Light</button>
          </div>
        </div>

        <div class="ap-row ap-row-col">
          <div class="ap-info">
            <span class="ap-label">Accent hue</span>
            <span class="ap-desc">The top bar, accents and highlights all follow this colour</span>
          </div>
          <div class="hue-row">
            <input
              v-model.number="accentHue"
              class="hue-range"
              type="range"
              min="0"
              max="360"
              step="1"
              aria-label="Accent hue"
            />
            <span class="hue-value">{{ accentHue }}&deg;</span>
          </div>
        </div>

        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Neutral colours</span>
            <span class="ap-desc">Drop the tint entirely and leave the interface grey</span>
          </div>
          <label class="toggle">
            <input type="checkbox" v-model="neutralColours" />
            <span class="toggle-slider"></span>
          </label>
        </div>

        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Reset</span>
            <span class="ap-desc">Back to MEW's default rose</span>
          </div>
          <button type="button" class="ap-btn" @click="resetColours">
            <i class="fa fa-rotate-left"></i> Reset
          </button>
        </div>
      </section>

      <section class="ap-card">
        <h3 class="ap-card-title">Typography</h3>
        <div class="ap-row ap-row-col">
          <div class="ap-info">
            <span class="ap-label">Interface font</span>
            <span class="ap-desc">Any font installed on this PC, or one you upload</span>
          </div>
          <div class="btn-row">
            <select v-model="fontChoice" class="ap-select">
              <option value="">Default</option>
              <option v-for="font in FONT_CHOICES" :key="font" :value="font">{{ font }}</option>
              <option v-if="uploadedFont" :value="CUSTOM_FONT_FAMILY">Uploaded font</option>
            </select>
            <button type="button" class="ap-btn" @click="pickFont">
              <i class="fa fa-upload"></i> Upload
            </button>
            <button v-if="uploadedFont" type="button" class="ap-btn" @click="removeFont">Remove</button>
          </div>
          <p v-if="fontNotice" class="ap-note">{{ fontNotice }}</p>
          <p class="font-sample" :style="{ fontFamily: fontPreview }">
            The quick brown fox jumps over the lazy dog
          </p>
        </div>
      </section>

      <section class="ap-card">
        <h3 class="ap-card-title">Backdrop</h3>

        <div class="ap-row ap-row-col">
          <div class="ap-info">
            <span class="ap-label">Background image</span>
            <span class="ap-desc">{{ backdropImage ? 'Custom image' : 'None — flat colour' }}</span>
          </div>
          <div class="btn-row">
            <button type="button" class="ap-btn" @click="pickBackdrop">
              <i class="fa fa-image"></i> Choose image
            </button>
            <button v-if="backdropImage" type="button" class="ap-btn" @click="backdropImage = ''">Remove</button>
          </div>
          <div
            v-if="backdropImage"
            class="bg-preview"
            :style="{ backgroundImage: `url(${backdropImage})` }"
          ></div>
        </div>

        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Glass panels</span>
            <span class="ap-desc">Blur whatever is behind the cards and the sidebar</span>
          </div>
          <label class="toggle">
            <input type="checkbox" v-model="glassPanels" />
            <span class="toggle-slider"></span>
          </label>
        </div>
      </section>

      <section class="ap-card">
        <h3 class="ap-card-title">Extras</h3>

        <div class="ap-row ap-row-col">
          <div class="ap-info">
            <span class="ap-label">Custom CSS</span>
            <span class="ap-desc">Fine-tune anything the theme doesn't expose</span>
          </div>
          <textarea
            v-model="customCss"
            class="ap-textarea"
            rows="4"
            spellcheck="false"
            placeholder=".home-card { border-radius: 12px; }"
          ></textarea>
          <div class="btn-row">
            <button type="button" class="ap-btn" @click="customCss = ''">Clear</button>
          </div>
        </div>

        <div class="ap-row">
          <div class="ap-info">
            <span class="ap-label">Appearance file</span>
            <span class="ap-desc">Export or load every look setting at once</span>
          </div>
          <div class="btn-row">
            <button type="button" class="ap-btn" @click="exportAppearance">Export</button>
            <button type="button" class="ap-btn" @click="pickAppearanceFile">Import</button>
          </div>
        </div>
      </section>

      <section class="ap-card">
        <h3 class="ap-card-title">Preview</h3>
        <div class="pv">
          <div class="pv-bar">
            <span class="pv-ball"></span>
            <span class="pv-title">HOME</span>
          </div>
          <div class="pv-body">
            <div class="pv-nav">
              <span class="pv-item active">Home</span>
              <span class="pv-item">Pack Porter</span>
              <span class="pv-item">Settings</span>
            </div>
            <div class="pv-main">
              <div class="pv-card">
                <div class="pv-card-head">Pack Porter</div>
                <p>Turn Java Edition packs into Bedrock packs.</p>
              </div>
            </div>
          </div>
        </div>
      </section>

      <input ref="backdropInput" type="file" accept="image/*" hidden @change="onBackdropFile" />
      <input ref="fontInput" type="file" accept=".ttf,.otf,.woff,.woff2" hidden @change="onFontFile" />
      <input ref="appearanceInput" type="file" accept="application/json,.json" hidden @change="onAppearanceFile" />
    </div>
  </div>
</template>

<style scoped>
.appearance-page {
  overflow-y: auto;
}

.ap-wrap {
  width: 100%;
  max-width: 560px;
  margin: 0 auto;
  padding: 1.5rem 1rem 2rem;
}

.ap-head {
  margin-bottom: 1.4rem;
  text-align: center;
}

.ap-head h2 {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-size: 1.5rem;
  font-weight: 700;
  color: var(--accent);
}

.ap-head p {
  margin-top: 0.2rem;
  font-size: 0.75rem;
  color: var(--text-dim);
}

.ap-card {
  border: 1px solid var(--border-default);
  background: var(--bg-body);
  padding: 0 1.1rem;
  margin-bottom: 0.9rem;
}

.ap-card-title {
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
  padding: 0.9rem 0 0.6rem;
  border-bottom: 1px solid var(--border-subtle);
}

.ap-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.8rem;
  padding: 0.75rem 0;
  border-bottom: 1px solid var(--border-subtle);
}

.ap-row:last-child {
  border-bottom: none;
}

.ap-row-col {
  flex-direction: column;
  align-items: flex-start;
  gap: 0.6rem;
}

.ap-info {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}

.ap-label {
  font-size: 0.875rem;
  color: var(--text-secondary);
  font-weight: 500;
}

.ap-desc {
  font-size: 0.75rem;
  color: var(--text-dim);
}

.ap-note {
  font-size: 0.75rem;
  color: var(--accent);
}

.seg {
  display: flex;
  flex-shrink: 0;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  overflow: hidden;
}

.seg button {
  padding: 0.3rem 0.8rem;
  background: var(--bg-input);
  border: none;
  color: var(--text-muted);
  font-family: var(--font-body);
  font-size: 0.75rem;
  cursor: pointer;
}

.seg button + button {
  border-left: 1px solid var(--border-strong);
}

.seg button.on {
  background: var(--accent-soft);
  color: var(--accent-light);
  font-weight: 600;
}

.hue-row {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  width: 100%;
}

.hue-range {
  appearance: none;
  -webkit-appearance: none;
  flex: 1;
  height: 10px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: linear-gradient(
    to right,
    hsl(0, 80%, 60%),
    hsl(60, 80%, 60%),
    hsl(120, 80%, 60%),
    hsl(180, 80%, 60%),
    hsl(240, 80%, 60%),
    hsl(300, 80%, 60%),
    hsl(360, 80%, 60%)
  );
  outline: none;
  cursor: pointer;
}

.hue-range::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--accent);
  border: 2px solid #fff;
  box-shadow: 0 0 0 1px var(--border-strong);
}

.hue-value {
  font-size: 0.75rem;
  color: var(--text-muted);
  min-width: 2.4rem;
  text-align: right;
}

.btn-row {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-wrap: wrap;
  width: 100%;
}

.ap-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 0.3rem;
  padding: 0.3rem 0.6rem;
  font-size: 0.75rem;
  border-radius: 4px;
  cursor: pointer;
  background: var(--bg-hover-2);
  color: var(--text-muted);
  border: 1px solid var(--border-strong);
}

.ap-btn:hover {
  background: var(--bg-hover-5);
}

.ap-select {
  flex: 1;
  min-width: 8rem;
  background: var(--bg-input);
  border: 1px solid var(--border-strong);
  color: var(--text-secondary);
  font-size: 0.8rem;
  padding: 0.35rem 0.5rem;
  border-radius: 4px;
  outline: none;
}

.ap-textarea {
  width: 100%;
  min-height: 84px;
  background: var(--bg-input);
  border: 1px solid var(--border-strong);
  border-radius: 4px;
  color: var(--text-secondary);
  font-family: Consolas, 'Cascadia Code', monospace;
  font-size: 0.75rem;
  line-height: 1.5;
  padding: 0.45rem 0.6rem;
  resize: vertical;
  outline: none;
}

.ap-textarea:focus {
  border-color: var(--accent);
}

.font-sample {
  width: 100%;
  padding: 0.55rem 0.7rem;
  border: 1px dashed var(--border-strong);
  border-radius: 4px;
  font-size: 0.95rem;
  color: var(--text-secondary);
  background: var(--bg-surface);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bg-preview {
  width: 100%;
  height: 72px;
  border-radius: 4px;
  border: 1px solid var(--border-default);
  background-size: cover;
  background-position: center;
}

.toggle {
  position: relative;
  display: inline-block;
  width: 40px;
  height: 22px;
  flex-shrink: 0;
}

.toggle input {
  opacity: 0;
  width: 0;
  height: 0;
}

.toggle-slider {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: var(--bg-hover-5);
  border: 1px solid var(--border-focus);
  border-radius: 22px;
}

.toggle-slider::before {
  content: '';
  position: absolute;
  width: 16px;
  height: 16px;
  left: 2px;
  bottom: 2px;
  background: var(--text-dim);
  border-radius: 50%;
}

.toggle input:checked + .toggle-slider {
  background: var(--accent-glow2);
  border-color: var(--accent);
}

.toggle input:checked + .toggle-slider::before {
  transform: translateX(18px);
  background: var(--accent);
}

/* ---- live preview ---- */
.pv {
  margin: 0.9rem 0 1.1rem;
  border: 1px solid var(--border-default);
  border-radius: 10px;
  overflow: hidden;
  background: var(--bg-surface);
}

.pv-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 26px;
  padding: 0 8px;
  background: var(--accent);
  border-bottom: 2px solid var(--accent-dark);
}

.pv-ball {
  width: 14px;
  height: 14px;
  background: url('/pokeball.png') center / 14px 14px no-repeat;
  filter: hue-rotate(var(--ball-hue, 0deg));
}

.pv-title {
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 10px;
  border-right: 2px solid var(--accent-dark);
  color: #fff;
  font-size: 0.6rem;
  font-weight: 700;
  letter-spacing: 0.12em;
}

.pv-body {
  display: flex;
  min-height: 76px;
}

.pv-nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 96px;
  flex-shrink: 0;
  padding: 8px 6px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-subtle);
}

.pv-item {
  position: relative;
  padding: 4px 8px;
  border-radius: 6px;
  font-size: 0.65rem;
  color: var(--text-muted);
}

.pv-item.active {
  background: var(--accent-soft);
  color: var(--accent-light);
  font-weight: 600;
}

.pv-item.active::before {
  content: '';
  position: absolute;
  left: -6px;
  top: 5px;
  bottom: 5px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: var(--accent);
}

.pv-main {
  flex: 1;
  padding: 10px;
}

.pv-card {
  border: 1px solid var(--panel-ring);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-surface);
}

.pv-card-head {
  padding: 5px 10px;
  background: var(--accent);
  color: #fff;
  font-size: 0.65rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.pv-card p {
  padding: 8px 10px;
  font-size: 0.68rem;
  color: var(--text-desc);
}
</style>
