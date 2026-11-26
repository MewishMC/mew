export function rgbToHsv(r, g, b) {
  r /= 255; g /= 255; b /= 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const d = max - min
  let h = 0
  const s = max === 0 ? 0 : d / max
  const v = max
  if (d !== 0) {
    if (max === r) h = ((g - b) / d + (g < b ? 6 : 0)) / 6
    else if (max === g) h = ((b - r) / d + 2) / 6
    else h = ((r - g) / d + 4) / 6
  }
  return [h * 360, s * 100, v * 100]
}

export function hsvToRgb(h, s, v) {
  h /= 360; s /= 100; v /= 100
  let r, g, b
  const i = Math.floor(h * 6)
  const f = h * 6 - i
  const p = v * (1 - s)
  const q = v * (1 - f * s)
  const t = v * (1 - (1 - f) * s)
  switch (i % 6) {
    case 0: r = v; g = t; b = p; break
    case 1: r = q; g = v; b = p; break
    case 2: r = p; g = v; b = t; break
    case 3: r = p; g = q; b = v; break
    case 4: r = t; g = p; b = v; break
    case 5: r = v; g = p; b = q; break
  }
  return [Math.round(r * 255), Math.round(g * 255), Math.round(b * 255)]
}

export function applyHsvShift(imageData, hueShift, satShift, brightShift) {
  const data = imageData.data
  for (let i = 0; i < data.length; i += 4) {
    const a = data[i + 3]
    if (a === 0) continue
    let [h, s, v] = rgbToHsv(data[i], data[i + 1], data[i + 2])
    h = ((h + hueShift) % 360 + 360) % 360
    s = Math.max(0, Math.min(100, s + satShift))
    v = Math.max(0, Math.min(100, v + brightShift))
    const [r, g, b] = hsvToRgb(h, s, v)
    data[i] = r
    data[i + 1] = g
    data[i + 2] = b
  }
  return imageData
}

// Pulls a texture toward one colour while keeping its shading: each pixel keeps
// its luminance and takes the target colour's hue/chroma. strength 0 leaves the
// pixels alone, 1 recolours them fully.
export function colorizeImageData(imageData, rgb, strength = 1) {
  const t = Math.max(0, Math.min(1, strength))
  if (t === 0) return imageData
  const [tr, tg, tb] = rgb
  const peak = Math.max(tr, tg, tb) || 1
  const nr = tr / peak
  const ng = tg / peak
  const nb = tb / peak
  const data = imageData.data
  for (let i = 0; i < data.length; i += 4) {
    if (data[i + 3] === 0) continue
    const lum = (0.299 * data[i] + 0.587 * data[i + 1] + 0.114 * data[i + 2]) / 255
    const r = Math.min(255, Math.round(nr * lum * 255))
    const g = Math.min(255, Math.round(ng * lum * 255))
    const b = Math.min(255, Math.round(nb * lum * 255))
    data[i] = Math.round(data[i] + (r - data[i]) * t)
    data[i + 1] = Math.round(data[i + 1] + (g - data[i + 1]) * t)
    data[i + 2] = Math.round(data[i + 2] + (b - data[i + 2]) * t)
  }
  return imageData
}

export function applySepia(imageData) {
  const data = imageData.data
  for (let i = 0; i < data.length; i += 4) {
    const a = data[i + 3]
    if (a === 0) continue
    const r = data[i]
    const g = data[i + 1]
    const b = data[i + 2]
    data[i] = Math.min(255, Math.round(0.393 * r + 0.769 * g + 0.189 * b))
    data[i + 1] = Math.min(255, Math.round(0.349 * r + 0.686 * g + 0.168 * b))
    data[i + 2] = Math.min(255, Math.round(0.272 * r + 0.534 * g + 0.131 * b))
  }
  return imageData
}

