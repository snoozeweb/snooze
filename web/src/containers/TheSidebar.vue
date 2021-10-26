<template>
  <CSidebar 
    fixed 
    :minimize="minimize"
    :show="show"
    @update:show="(value) => $store.commit('set', ['sidebarShow', value])"
  >
    <CSidebarBrand class="d-md-down-none" to="/">
      <img
        class="c-sidebar-brand-full my-2" 
        name="logo" 
        size="custom-size"
	src="img/logo_white.png"
        :height="60" 
        viewBox="0 0 556 234"
      />
      <img
        class="c-sidebar-brand-minimized my-2" 
        name="logo" 
        size="custom-size"
	src="img/logo-symbol.png"
        :height="32" 
        viewBox="0 0 110 134"
      />
    </CSidebarBrand>

    <CRenderFunction flat :content-to-render="nav_filter($options.nav)"/>
    <CSidebarMinimizer
      class="d-md-down-none"
      @click.native="$store.commit('set', ['sidebarMinimize', !minimize])"
    />
  </CSidebar>
</template>

<script>
import nav from './_nav'
import jwt_decode from "jwt-decode"

export default {
  name: 'TheSidebar',
  nav,
  mounted () {
  },
  methods: {
    nav_filter(nav_el) {
      var token = localStorage.getItem('snooze-token')
      var permissions = []
      if (token) {
        permissions = jwt_decode(token).user.permissions
      }
      if (permissions) {
        var nav_items = []
        nav_el[0]._children.forEach(function(item) {
          if (item.permissions) {
            if (permissions.filter(cap => cap == 'rw_all' || cap == 'ro_all' || item.permissions.includes(cap)).length > 0) {
              nav_items.push(item)
            }
          } else {
            nav_items.push(item)
          }
        })
        var nav_children = []
        nav_items.forEach(function (item, i) {
          if (item._name != 'CSidebarNavTitle' || nav_items[i+1]._name != 'CSidebarNavTitle') {
            nav_children.push(item)
          }
        })
        return [{'_name': "CSidebarNav", '_children': nav_children}]
      } else {
        return []
      }
    }
  },
  computed: {
    show () {
      return this.$store.state.sidebarShow 
    },
    minimize () {
      return this.$store.state.sidebarMinimize 
    }
  }
}
</script>
