import { ref, watch } from 'vue'

// Every look setting lives here so the Appearance page, the startup script in
// index.html and the rest of the app all read the same values. They are kept
// as one JSON blob so export/import and the pre-paint script stay in sync.
const KEY = 'mew_appearance'

const ACCENT_HUE_DEFAULT = 336
export const CUSTOM_FONT_FAMILY = 'MEW Custom Font'

const DEFAULT_STACK = "'Segoe UI', system-ui, -apple-system, sans-serif"

function readSaved() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '{}')
    return raw && typeof raw === 'object' ? raw : {}
  } catch (e) {
    return {}
  }
}

const saved = readSaved()
const legacyHue = parseInt(localStorage.getItem('mew_accent_hue') || '', 10)

export const accentHue = ref(
  typeof saved.hue === 'number' && saved.hue >= 0 && saved.hue <= 360
    ? Math.round(saved.hue)
    : Number.isFinite(legacyHue)
      ? legacyHue
      : ACCENT_HUE_DEFAULT
)
export const neutralColours = ref(
  saved.neutral === true || localStorage.getItem('mew_neutral') === '1'
)
export const sidebarSide = ref(saved.side === 'right' ? 'right' : 'left')
export const glassPanels = ref(saved.glass === true)
export const backdropImage = ref(typeof saved.backdrop === 'string' ? saved.backdrop : '')
export const backdropBlur = ref(
  typeof saved.blur === 'number' && saved.blur >= 0 && saved.blur <= 24
    ? Math.round(saved.blur)
    : 0
)
export const interfaceFont = ref(typeof saved.font === 'string' ? saved.font : '')
export const uploadedFont = ref(typeof saved.fontData === 'string' ? saved.fontData : '')
export const customCss = ref(typeof saved.css === 'string' ? saved.css : '')

export const currentTheme = ref('dark')

export function applyTheme(value) {
  const next = value === 'light' ? 'light' : 'dark'
  currentTheme.value = next
  document.documentElement.dataset.theme = next
}

export function appearanceSnapshot() {
  return {
    hue: accentHue.value,
    neutral: neutralColours.value,
    side: sidebarSide.value,
    glass: glassPanels.value,
    backdrop: backdropImage.value,
    blur: backdropBlur.value,
    font: interfaceFont.value,
    fontData: uploadedFont.value,
    css: customCss.value,
  }
}

function persistAppearance() {
  try {
    localStorage.setItem(KEY, JSON.stringify(appearanceSnapshot()))
  } catch (e) {
    // Over the storage quota (very large image or font): the look still holds
    // for this run, it just will not be remembered.
  }
}

const loadedFaces = new Set()

function registerUploadedFont() {
  if (!uploadedFont.value || loadedFaces.has(CUSTOM_FONT_FAMILY)) return
  try {
    const face = new FontFace(CUSTOM_FONT_FAMILY, `url(${uploadedFont.value})`)
    loadedFaces.add(CUSTOM_FONT_FAMILY)
    face
      .load()
      .then((f) => document.fonts.add(f))
      .catch(() => {})
  } catch (e) {
    // Unsupported font data: fall back to the stack below.
  }
}

function ensureStyleTag() {
  let el = document.getElementById('mew-custom-css')
  if (!el) {
    el = document.createElement('style')
    el.id = 'mew-custom-css'
    document.head.appendChild(el)
  }
  return el
}

function fontStack() {
  if (uploadedFont.value) return `"${CUSTOM_FONT_FAMILY}", ${DEFAULT_STACK}`
  if (interfaceFont.value) return `"${interfaceFont.value}", ${DEFAULT_STACK}`
  return DEFAULT_STACK
}

export function applyAppearance() {
  const root = document.documentElement
  root.style.setProperty('--hue', String(accentHue.value))
  root.dataset.neutral = neutralColours.value ? '1' : '0'
  root.dataset.glass = glassPanels.value ? '1' : '0'
  root.dataset.side = sidebarSide.value
  root.style.setProperty(
    '--app-backdrop',
    backdropImage.value ? `url("${backdropImage.value}")` : 'none'
  )
  root.style.setProperty(
    '--backdrop-blur',
    `${backdropImage.value ? backdropBlur.value : 0}px`
  )

  const stack = fontStack()
  root.style.setProperty('--font-body', stack)
  root.style.setProperty('--font-pixel', stack)

  registerUploadedFont()
  ensureStyleTag().textContent = customCss.value
}

export function resetColours() {
  accentHue.value = ACCENT_HUE_DEFAULT
  neutralColours.value = false
}

watch(
  [
    accentHue,
    neutralColours,
    sidebarSide,
    glassPanels,
    backdropImage,
    backdropBlur,
    interfaceFont,
    uploadedFont,
    customCss,
  ],
  () => {
    applyAppearance()
    persistAppearance()
  }
)

applyAppearance()
