import { createI18n } from 'vue-i18n'
import en from './en.js'
import zh from './zh.js'

function detectLocale() {
  const saved = localStorage.getItem('locale')
  if (saved && ['en', 'zh'].includes(saved)) return saved
  const lang = navigator.language || ''
  return lang.startsWith('zh') ? 'zh' : 'en'
}

const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: 'en',
  messages: { en, zh },
})

export default i18n
