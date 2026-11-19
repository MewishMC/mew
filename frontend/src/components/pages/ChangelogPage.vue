<script setup>
import { ref, computed, onMounted } from 'vue'
import { GetChangelog, OpenDownloadLink } from '../../../wailsjs/go/main/App'

const changelog = ref([])
const loading = ref(true)

function openGitHub(url) {
  if (url) OpenDownloadLink(url)
}

// A release lists its tag's commits; if GitHub gave us none (rate limit, no
// previous tag) fall back to the lines of the release body.
function bodyLines(body) {
  if (!body) return []
  return body
    .split('\n')
    .map((line) => line.trim())
    .filter(
      (line) =>
        line &&
        !line.startsWith('#') &&
        !/^\*\*Full Changelog\*\*:?/i.test(line) &&
        !/^Full Changelog:?/i.test(line) &&
        !/^what'?s changed$/i.test(line)
    )
    .map((line) => line.replace(/^\*\s+/, '').replace(/^-\s+/, ''))
}

// Tags with nothing written in them are left out: the page only shows releases
// that have changes to read.
const entries = computed(() =>
  changelog.value
    .map((entry) => {
      const commits = entry.commits || []
      return {
        ...entry,
        items: commits.length > 0 ? commits : bodyLines(entry.body),
        fromCommits: commits.length > 0,
      }
    })
    .filter((entry) => entry.items.length > 0)
)

onMounted(async () => {
  try {
    const list = await GetChangelog()
    if (list) changelog.value = list
  } catch (e) {
    console.error('Failed to load changelog:', e)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="page active-page changelog-page">
    <div class="cl-content">
      <section class="cl-card page-bar-card">
        <h2 class="card-title"><i class="fa fa-clipboard-list"></i> Changelog</h2>

        <p v-if="loading" class="cl-empty">Loading releases from GitHub...</p>
        <p v-else-if="entries.length === 0" class="cl-empty">No changelog entries yet.</p>

        <div v-for="entry in entries" :key="entry.tag" class="cl-entry">
          <div class="cl-entry-head">
            <span class="cl-tag">v{{ entry.tag }}</span>
            <span v-if="entry.date" class="cl-date">{{ entry.date }}</span>
            <a class="cl-link" @click.prevent="openGitHub(entry.url)">View on GitHub</a>
          </div>

          <ul class="cl-commits">
            <li v-for="(item, i) in entry.items" :key="i" :class="{ 'cl-body-line': !entry.fromCommits }">
              {{ item }}
            </li>
          </ul>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.changelog-page {
  justify-content: flex-start;
  align-items: center;
  padding: 2.5rem 1rem;
  overflow-y: auto;
}

.cl-content {
  max-width: 1080px;
  width: 100%;
}

.cl-empty {
  font-size: 0.85rem;
  color: var(--text-dim);
}

/* One box per release, so each tag reads as its own block. */
.cl-entry {
  background: var(--bg-body);
  border: 1px solid var(--border-default);
  border-radius: 10px;
  padding: 0.9rem 1rem 1rem;
  margin-top: 0.75rem;
}

.cl-entry:first-of-type {
  margin-top: 0.25rem;
}

.cl-entry-head {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin-bottom: 0.55rem;
}

.cl-tag {
  font-weight: 700;
  font-size: 0.9rem;
  color: var(--accent);
  letter-spacing: 0.01em;
}

.cl-date {
  font-size: 0.72rem;
  color: var(--text-dim);
}

.cl-link {
  margin-left: auto;
  font-size: 0.72rem;
  color: var(--accent);
  text-decoration: none;
  cursor: pointer;
}

.cl-link:hover {
  text-decoration: underline;
}

.cl-commits {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.cl-commits li {
  position: relative;
  padding-left: 0.9rem;
  font-size: 0.8rem;
  line-height: 1.5;
  color: var(--text-desc);
  overflow-wrap: anywhere;
}

.cl-commits li::before {
  content: '';
  position: absolute;
  left: 0.1rem;
  top: 0.55em;
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--accent);
  opacity: 0.8;
}

/* Release-note lines used as a fallback read as paragraphs, not commits. */
.cl-commits li.cl-body-line {
  padding-left: 0;
}

.cl-commits li.cl-body-line::before {
  content: none;
}
</style>
