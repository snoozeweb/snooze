import 'core-js/stable'
import Vue from 'vue'
import BootstrapVue from 'bootstrap-vue'
import App from './App'
import router from './router'
import CoreuiVue from '@coreui/vue'
//import { iconsSet as icons } from './assets/icons/icons.js'
import store from './store'
import velocityPlugin from 'velocity-vue'

Vue.config.performance = true
Vue.use(CoreuiVue)
Vue.use(BootstrapVue)
Vue.use(velocityPlugin)
Vue.prototype.$log = console.log.bind(console)

export const app = new Vue({
  el: '#app',
  router,
  store,
//  icons,
  template: '<App/>',
  components: {
    App
  }
})
