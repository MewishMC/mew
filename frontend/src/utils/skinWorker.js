// The player renderer: bedrock-skin (the same pixels as its Go and Rust
// versions) in a Web Worker, so drawing never stalls the page. It takes the
// textures the backend found (GetPlayerTextures) and a render request in the
// shape the Pack Viewer has always sent, and answers with data URIs.
import {
  MOTIONS,
  armorSet,
  decodeImage,
  encodePNG,
  exampleAnimations,
  parseMotion,
  prepareFrames,
  renderGIF,
  renderItemGIF,
  renderItemPNG,
  render,
} from 'bedrock-skin'

const SLIM = 'geometry.humanoid.customSlim'
const PARTS = new Set(['head', 'body', 'rightarm', 'leftarm', 'rightleg', 'leftleg'])

function clamp(v, lo, hi) { return Math.min(hi, Math.max(lo, v)) }
function clampInt(v, lo, hi, def) { return clamp(Math.round(Number(v) || def), lo, hi) }
function nonzeroClamp(v, lo, hi) { return !v ? 0 : clamp(Number(v), lo, hi) }

// --- data URIs ---

function uriBytes(uri) {
  const bin = atob(uri.slice(uri.indexOf(',') + 1))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function bytesURI(bytes, mime) {
  let bin = ''
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000))
  return 'data:' + mime + ';base64,' + btoa(bin)
}

const pngURI = (bytes) => bytesURI(bytes, 'image/png')
const gifURI = (bytes) => bytesURI(bytes, 'image/gif')

// Decoded textures, by their data URI. A pack's textures repeat across every
// render while the viewer is open, so each is decoded once.
const decoded = new Map()
function image(uri) {
  if (!uri) return undefined
  let img = decoded.get(uri)
  if (!img) {
    img = decodeImage(uriBytes(uri))
    if (decoded.size >= 64) decoded.delete(decoded.keys().next().value)
    decoded.set(uri, img)
  }
  return img
}

// --- requests ---

function camera(c) {
  if (!c) return undefined
  return {
    yaw: Number(c.yaw) || 0,
    pitch: clamp(Number(c.pitch) || 0, -89, 89),
    fov: nonzeroClamp(c.fov, 10, 90),
    margin: nonzeroClamp(c.margin, 0.3, 4),
  }
}

// adjust takes a hand adjustment in either the Go field names the viewer has
// always sent ({ Offset, Rotation, Scale }) or bedrock-skin's own.
function adjust(a) {
  if (!a) return undefined
  return {
    offset: a.offset ?? a.Offset,
    rotation: a.rotation ?? a.Rotation,
    scale: a.scale ?? a.Scale,
  }
}

function held(h, a) {
  return h && h.item ? { item: image(h.item), flat: h.flat, adjust: adjust(a) } : undefined
}

function parts(p) {
  if (!p) return undefined
  for (const name of Object.keys(p)) {
    if (!PARTS.has(name.trim().toLowerCase())) throw new Error('unknown body part: ' + name)
  }
  return p
}

// options turns textures and a request into bedrock-skin's render options.
function options(tex, req) {
  const opts = {
    texture: image(tex.skin),
    identifier: tex.slim ? SLIM : '',
    view: req.view || 'body',
    angle: req.angle || 'front',
    size: clampInt(req.size, 32, 1024, 512),
    scale: { model: req.modelSize || 0, parts: parts(req.parts) },
    hideSkin: !!req.hideSkin,
    camera: camera(req.camera),
  }
  if (tex.layer1 && tex.layer2) opts.armor = armorSet(image(tex.layer1), image(tex.layer2))
  if (tex.elytra) opts.armor = { ...(opts.armor || {}), elytra: image(tex.elytra) }
  opts.rightHand = held(tex.right, req.right && req.right.adjust)
  opts.leftHand = held(tex.left, req.left && req.left.adjust)
  return opts
}

function animator(name) {
  name = (name || '').trim()
  if (!name) throw new Error('no animation requested')
  try { return parseMotion(name) } catch {}
  const anim = exampleAnimations().get(name)
  if (!anim) throw new Error('unknown animation: ' + name)
  return anim
}

function animationOptions(tex, req) {
  return {
    ...options(tex, req),
    animation: animator(req.animation),
    fps: clampInt(req.fps, 1, 60, 15),
    frames: clampInt(req.frames, 0, 480, 0),
  }
}

// Textures sent once and named by id, so a frame request does not carry
// every texture again 60 times a second.
const texById = new Map()
function textures(args) {
  if (args.tex) {
    if (args.texId) {
      if (texById.size >= 16) texById.delete(texById.keys().next().value)
      texById.set(args.texId, args.tex)
    }
    return args.tex
  }
  const tex = texById.get(args.texId)
  if (!tex) throw new Error('unknown textures')
  return tex
}

// The most recent prepared animation. The viewer shows one appearance at a
// time, so one slot draws every camera move from the one frame set. The
// camera, frame and size are given per draw, so they are not in the key.
let framesKey = ''
let framesSet = null
function prepared(tex, req, texId) {
  const key = JSON.stringify([texId || tex, { ...req, camera: null, frame: 0, size: 0 }])
  if (key !== framesKey) {
    framesSet = prepareFrames(animationOptions(tex, req))
    framesKey = key
  }
  return framesSet
}

function frameIndex(i, n) { return n ? ((Math.trunc(Number(i) || 0) % n) + n) % n : 0 }

// still draws one picture; with an animation, its frame req.frame (wrapped
// into range), framed by the camera the whole animation shares.
function still(args) {
  const tex = textures(args)
  const req = args.req
  if ((req.animation || '').trim()) {
    const set = prepared(tex, req, args.texId)
    return set.draw(frameIndex(req.frame, set.length), clampInt(req.size, 32, 1024, 512), camera(req.camera))
  }
  return render(options(tex, req))
}

const ops = {
  // render draws one still as a PNG data URI.
  render(args) {
    return pngURI(encodePNG(still(args)))
  },

  // draw draws one still as raw RGBA pixels, for the live view: no PNG to
  // encode here or decode on the page, so a frame costs only its drawing.
  draw(args) {
    const img = still(args)
    return { width: img.width, height: img.height, data: img.data }
  },

  gif(args) {
    const tex = textures(args)
    const req = args.req
    // A GIF's frame delay is in hundredths of a second, and players treat
    // anything under 2 as slow, so a GIF runs at 50 FPS at most.
    const opts = animationOptions(tex, { ...req, fps: Math.min(clampInt(req.fps, 1, 60, 15), 50) })
    opts.size = Math.min(opts.size, 512)
    return gifURI(renderGIF(opts))
  },

  item({ item, angle, size }) {
    return pngURI(renderItemPNG({ item: image(item), angle: angle || 'front', size: clampInt(size, 32, 512, 256) }))
  },

  itemSpin({ item, size }) {
    return gifURI(renderItemGIF({ item: image(item), size: clampInt(size, 32, 512, 256) }))
  },

  // animations lists the motions first, then the bundled examples by name.
  animations() {
    return [...MOTIONS, ...[...exampleAnimations().keys()].sort()]
  },
}

self.onmessage = (e) => {
  const { id, op, args } = e.data
  try {
    const result = ops[op](args || {})
    // Raw pixels move to the page rather than being copied.
    const buf = result && result.data && result.data.buffer
    if (buf && buf.byteLength === result.data.byteLength) self.postMessage({ id, result }, [buf])
    else self.postMessage({ id, result })
  } catch (err) {
    self.postMessage({ id, error: String(err && err.message ? err.message : err) })
  }
}
