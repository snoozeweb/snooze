<template>
  <CHeader position="sticky" class="pt-0 pb-0" style="min-height: 1rem">
    <CContainer fluid>
      <CHeaderToggler class="ps-1" @click="$store.commit('toggleSidebar')">
        <i class="la la-bars la-lg"></i>
      </CHeaderToggler>
      <CHeaderBrand class="mx-auto d-lg-none" to="/">
        <i class="la la-bars la-lg"></i>
      </CHeaderBrand>
      <CHeaderNav class="d-none d-md-flex me-auto">
        <WidgetList class="ms-3"/>
      </CHeaderNav>
      <CHeaderNav class="d-none d-md-flex me-auto" v-if="router.currentRoute.path == '/record'">
        <Environment />
      </CHeaderNav>
      <CHeaderNav>
        <AppHeaderDropdownAccnt />
      </CHeaderNav>
    </CContainer>
  </CHeader>
</template>

<script>
import AppHeaderDropdownAccnt from './AppHeaderDropdownAccnt'
import WidgetList from '@/components/WidgetList'
import Environment from '@/components/Environment.vue'
import router from '@/router'

export default {
  name: 'AppHeader',
  components: {
    AppHeaderDropdownAccnt,
    WidgetList,
    Environment,
  },
  data() {
    return {
      router: router,
    }
  },
}
</script>
