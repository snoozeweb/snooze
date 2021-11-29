import { createApp } from 'vue'
import App from './App'
import router from './router'
import CoreuiVue from '@coreui/vue'
//import { iconsSet as icons } from './assets/icons/icons.js'
import store from './store'
import contextmenu from 'v-contextmenu'
import VueSafeHTML from 'vue-safe-html'
import 'chartjs-adapter-moment'
import mitt from 'mitt'
const emitter = mitt()

export const app = createApp(App)
app.config.warnHandler = () => null
app.config.performance = true
app.config.globalProperties.emitter = emitter
app.use(store)
app.use(router)
app.use(CoreuiVue)
app.use(VueSafeHTML)
app.use(contextmenu)

export const vm = app.mount('#app')
