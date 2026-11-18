import { reactive } from 'vue'
import { StartUpdate } from '../../wailsjs/go/main/App'

export const updaterState = reactive({
  state: 'idle',
  error: '',
  busy: false
})

// startUpdate hands the update to update.exe, which downloads the new build,
// swaps it in and starts MEW again. The window closing is part of the update.
export async function startUpdate() {
  if (updaterState.busy) return
  updaterState.busy = true
  updaterState.error = ''
  updaterState.state = 'starting'
  try {
    await StartUpdate()
    updaterState.state = 'restarting'
  } catch (e) {
    updaterState.error = (e && e.message) || String(e) || 'Update failed'
    updaterState.state = 'error'
  } finally {
    updaterState.busy = false
  }
}
