import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'

import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { setLocaleProvider } from './api/client'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './stores/auth'
import { permDirective } from './directives/perm'

import 'element-plus/dist/index.css'
import './styles/index.scss'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(i18n)

// 请求层按当前界面语言发送协商头；用回调避免 api/client 与 i18n 循环依赖。
setLocaleProvider(() => i18n.global.locale.value)

// 令牌彻底失效（连刷新都失败）时统一回登录页
setUnauthorizedHandler(() => {
  const auth = useAuthStore(pinia)
  auth.reset()
  const current = router.currentRoute.value
  if (current.name !== 'login') {
    router.replace({ name: 'login', query: { redirect: current.fullPath } })
  }
})

app.use(router)
app.use(ElementPlus)
app.directive('perm', permDirective)

app.mount('#app')
