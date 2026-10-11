// The player renderer the Pack Viewer calls: the same functions, in the same
// shapes, the Go backend used to answer. The backend now only finds the
// textures (GetPlayerTextures, GetItemTexture); bedrock-skin draws them in a
// Web Worker (skinWorker.js).
import { GetPlayerTextures, GetPackThumbnailTextures, GetItemTexture } from '../../wailsjs/go/main/App'

let worker = null
let nextId = 0
const pending = new Map()

function call(op, args) {
  if (!worker) {
    worker = new Worker(new URL('./skinWorker.js', import.meta.url), { type: 'module' })
    worker.onmessage = (e) => {
      const { id, result, error } = e.data
      const p = pending.get(id)
      if (!p) return
      pending.delete(id)
      if (error !== undefined) p.reject(new Error(error))
      else p.resolve(result)
    }
    worker.onerror = (e) => {
      for (const p of pending.values()) p.reject(new Error(e.message || 'skin worker failed'))
      pending.clear()
      sentTex.clear()
      worker = null
    }
  }
  const id = ++nextId
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    worker.postMessage({ id, op, args })
  })
}

// The textures for the player being shown, by what it wears. A camera move
// re-renders with the same textures, so they are fetched once; anything that
// may change them (a new skin, a vanilla texture arriving) calls
// invalidatePlayerTextures.
const textures = new Map()

function textureRequest(req) {
  return {
    pack: req.pack || '',
    base: req.base || '',
    model: req.model || '',
    material: req.material || '',
    elytra: !!req.elytra,
    right: (req.right && req.right.item) || '',
    left: (req.left && req.left.item) || '',
  }
}

let nextTexId = 0
const sentTex = new Set()

// playerTextures resolves to { texId, tex } for a request.
function playerTextures(req) {
  const tr = textureRequest(req)
  const key = JSON.stringify(tr)
  let p = textures.get(key)
  if (!p) {
    const texId = ++nextTexId
    p = GetPlayerTextures(tr).then(tex => ({ texId, tex }))
    p.catch(() => textures.delete(key))
    if (textures.size >= 16) textures.delete(textures.keys().next().value)
    textures.set(key, p)
  }
  return p
}

export function invalidatePlayerTextures() {
  textures.clear()
}

// withTex runs a player op, naming the textures by id and sending them only
// the first time; a worker that has let them go is sent them again.
async function withTex(op, req) {
  const { texId, tex } = await playerTextures(req)
  if (sentTex.has(texId)) {
    try {
      return await call(op, { texId, req })
    } catch (e) {
      if (e.message !== 'unknown textures') throw e
    }
  }
  sentTex.add(texId)
  return call(op, { texId, tex, req })
}

// RenderSkin renders one PNG of the player, as a data URI. With an animation
// it draws frame req.frame, framed by the camera the whole animation shares.
export async function RenderSkin(req) {
  return withTex('render', req)
}

// DrawSkin is RenderSkin as raw pixels, { width, height, data }, for the live
// view to put straight on a canvas.
export async function DrawSkin(req) {
  return withTex('draw', req)
}

// RenderSkinGIF renders an animation as a looping GIF data URI.
export async function RenderSkinGIF(req) {
  return withTex('gif', req)
}

// ListAnimations returns the motions, then the bundled example animations.
export function ListAnimations() {
  return call('animations')
}

// RenderItem renders one item extruded, front or iso.
export async function RenderItem(pack, base, item, angle, size) {
  return call('item', { item: await GetItemTexture(pack, base, item), angle, size })
}

// RenderItemSpin renders one item turning once, as a GIF data URI.
export async function RenderItemSpin(pack, base, item, size) {
  return call('itemSpin', { item: await GetItemTexture(pack, base, item), size })
}

// GetPackSkinThumbnails renders each pack card's picture: the pack's skin in
// its diamond armor holding its diamond sword, from the iso angle. Keyed by
// pack folder name.
export async function GetPackSkinThumbnails(packNames, base) {
  const all = (await GetPackThumbnailTextures(packNames, base)) || {}
  const out = {}
  await Promise.all(Object.entries(all).map(async ([name, tex]) => {
    try {
      out[name] = await call('render', { tex, req: { angle: 'iso', size: 96 } })
    } catch {}
  }))
  return out
}
