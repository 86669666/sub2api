import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../frontend/src/i18n/locales/en'
import Fixture from './visual-fixture.vue'
// Load the exact CSS links emitted by the production build, not a substitute theme.
const response = await fetch('/', { cache: 'no-store' })
if (!response.ok) throw new Error('production index unavailable')
const html = new DOMParser().parseFromString(await response.text(), 'text/html')
const hrefs = [...html.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]')].map(link => link.getAttribute('href')!)
if (!hrefs.length || hrefs.some(href => !/^\/assets\/[A-Za-z0-9_.-]+\.css$/.test(href))) throw new Error('production CSS identity invalid')
await Promise.all(hrefs.map(href => new Promise<void>((resolve, reject) => {
 const link = document.createElement('link'); link.rel = 'stylesheet'; link.href = href
 link.onload = () => resolve(); link.onerror = () => reject(new Error('production CSS load failed'))
 document.head.appendChild(link)
})))
Object.assign(window, { visualCSS: hrefs })
document.documentElement.classList.toggle('dark', localStorage.getItem('theme') === 'dark')
createApp(Fixture).use(createI18n({ legacy: false, locale: 'en', messages: { en } })).mount('#visual-root')
