<template>
  <CHeader fixed with-subheader light>
    <CToggler
      in-header
      class="ml-3 d-lg-none"
      @click="$store.commit('toggleSidebarMobile')"
    />
    <CToggler
      in-header
      class="ml-3 d-md-down-none"
      @click="$store.commit('toggleSidebarDesktop')"
    />
    <CHeaderBrand class="mx-auto d-lg-none" to="/">
      <CIcon name="logo" height="48" alt="Logo"/>
    </CHeaderBrand>
    <CHeaderNav class="d-md-down-none mr-auto">
      <WidgetList class="ml-3"/>
    </CHeaderNav>
    <CHeaderNav class="d-md-down-none mr-auto" v-if="router.currentRoute.path == '/record'">
      <Environment />
    </CHeaderNav>
    <CHeaderNav class="mr-4">
      <TheHeaderDropdownAccnt/>
    </CHeaderNav>
  </CHeader>
</template>

<script>
import TheHeaderDropdownAccnt from './TheHeaderDropdownAccnt'
import WidgetList from '@/components/WidgetList'
import Environment from '@/components/Environment.vue'
import router from '@/router'

export default {
  name: 'TheHeader',
  components: {
    TheHeaderDropdownAccnt,
    WidgetList,
    Environment,
  },
  data() {
    return {
      router: router,
    }
  },
  mounted() {
  },
  methods: {
  },
}
</script>
