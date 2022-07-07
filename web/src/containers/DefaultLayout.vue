<template>
  <div>
    <AppSidebar />
    <div class="wrapper d-flex flex-column min-vh-100 bg-light">
      <AppHeader />
      <div class="body flex-grow-1">
        <CContainer lg class="p-2 mw-100">
          <router-view v-slot="{ Component }">
            <transition name="mode-fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </CContainer>
      </div>
      <AppFooter />
    </div>
  </div>
</template>
<script>
import { CContainer } from '@coreui/vue'
import AppFooter from '@/containers/AppFooter.vue'
import AppHeader from '@/containers/AppHeader.vue'
import AppSidebar from '@/containers/AppSidebar.vue'
export default {
  name: 'DefaultLayout',
  components: {
    AppFooter,
    AppHeader,
    AppSidebar,
    CContainer,
  },
}
</script>

<style scoped>
.mode-fade-enter-active {
  transition: opacity .3s ease
}

.mode-fade-leave-active {
  transition: opacity .15s ease
}

.mode-fade-enter-from, .mode-fade-leave-to {
  opacity: 0
}
</style>
