<template>
  <CSidebar
    position="fixed"
    :unfoldable="sidebarUnfoldable"
    :visible="sidebarVisible"
    @visible-change="
      (event) =>
        $store.commit({
          type: 'updateSidebarVisible',
          value: event,
        })
    "
  >
    <CSidebarBrand class="d-md-down-none" to="/">
      <img
        class="sidebar-brand-full my-2"
        name="logo"
        size="custom-size"
      	src="img/logo_white.png"
        :height="60"
        viewBox="0 0 556 234"
      />
      <img
        class="sidebar-brand-narrow my-2"
        name="logo"
        size="custom-size"
	src="img/logo-symbol.png"
        :height="32"
        viewBox="0 0 110 134"
      />
    </CSidebarBrand>
    <AppSidebarNav />
    <CSidebarToggler
      class="d-none d-lg-flex"
      @click="$store.commit('toggleUnfoldable')"
    />
  </CSidebar>
</template>

<script>
import { computed } from 'vue'
import { useStore } from 'vuex'
import { AppSidebarNav } from './AppSidebarNav'

export default {
  name: 'AppSidebar',
  components: {
    AppSidebarNav,
  },
  setup() {
    const store = useStore()
    return {
      sidebarUnfoldable: computed(() => store.state.sidebarUnfoldable),
      sidebarVisible: computed(() => store.state.sidebarVisible),
    }
  },
}
</script>
