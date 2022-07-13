<template>
  <CButtonGroup role="group" class="pb-1 m-auto">
    <CButton
        v-for="(tab, index) in tabs"
        v-bind:key="tab.title"
        :style="(index == tab_index) ? gen_color(tab.color) : gen_color_outline(tab.color, 2)"
        v-on:click="change_tab(tab)"
    >
      {{ tab.name }}
    </CButton>
    <CButton v-if="is_admin()" color="secondary" @click="go_settings"><i class="la la-cog la-lg"></i></CButton>
  </CButtonGroup>
</template>

<script>

import { gen_color, gen_color_outline } from '@/utils/colors'
import { get_data } from '@/utils/api'
import { getStyle } from '@coreui/utils/src'

export default {
  name: 'Environment',
  props: {
    data: {
      // Array (it accepts String for recursion, but is not meant to be used like this)
      type: Array,
    },
    colorize: {
      // Assign random colors
      type: Boolean,
    },
  },
  data() {
    return {
      gen_color: gen_color,
      gen_color_outline: gen_color_outline,
      get_data: get_data,
      tabs: [],
      all_tab: {'name': 'All', 'filter': {}},
      tab_index: 0,
      filter: {},
    }
  },
  mounted() {
    this.all_tab.color = getStyle('--primary') || '#304ffe'
    this.tabs = [this.all_tab]
    this.get_data('environment', [], {'orderby': 'tree_order', 'asc': true}, this.get_data_response)
  },
  methods: {
    change_tab(tab, refresh = true) {
      this.tab_index = this.tabs.indexOf(tab)
      this.filter = tab.filter
      if (tab.handler) {
        tab.handler(tab)
      }
      this.emitter.emit('environment_change_tab', tab)
      //if (refresh) {
      //  this.refreshTable()
      //  this.add_history()
      //}
    },
    get_data_response(response) {
      if (response.data) {
        this.tabs = [this.all_tab]
        var rows = response.data.data || []
        rows.forEach(row => {
          this.tabs.push(row)
        })
      }
      this.reload()
    },
    reload() {
      var env_name = decodeURIComponent(this.$route.query.env_name)
      var tab
      if (env_name != undefined) {
        tab = this.tabs.filter(t => t.name == env_name)[0]
      }
      if (tab == undefined) {
        tab = this.tabs[0]
      }
      this.tab_index = this.tabs.indexOf(tab)
      this.filter = tab.filter
    },
    is_admin() {
      var permissions = localStorage.getItem('permissions') || []
      return permissions.includes('rw_all') || permissions.includes('rw_environment')
    },
    go_settings() {
      this.$router.push('/environment')
    }
  },
  watch: {
    $route() {
      this.$nextTick(this.reload);
    }
  },
}
</script>
