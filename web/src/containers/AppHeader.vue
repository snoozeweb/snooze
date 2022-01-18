<template>
  <CHeader position="sticky" class="pt-0 pb-0" style="min-height: 1rem">
    <CContainer fluid>
      <CCol md="auto">
        <CHeaderToggler class="ps-1 pe-4" @click="$store.commit('toggleSidebar')">
          <i class="la la-bars la-lg"></i>
        </CHeaderToggler>
      </CCol>
      <CCol>
        <CHeaderBrand class="mx-auto d-lg-none d-flex" to="/">
          <img
            class="sidebar-brand-narrow my-2 m-auto"
            name="logo"
            size="custom-size"
            src="img/logo.png"
            :height="32"
          />
        </CHeaderBrand>
        <div class="d-flex flex-wrap pt-1">
          <WidgetList />
          <Environment  v-if="router.currentRoute.path == '/record'"/>
        </div>
      </CCol>
      <CCol md="auto">
        <CHeaderNav>
          <AppHeaderDropdownAccnt />
        </CHeaderNav>
      </CCol>
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
