<template>
  <CButtonGroup role="group" class="pb-1 m-auto">
    <CButton
        v-on:click="select_all"
        :style="show_all ? gen_color(all_tab.color) : gen_color_outline(all_tab.color, 2)"
    >
      All
    </CButton>
    <CButton
        v-for="(tab, index) in tabs"
        v-bind:key="tab.title"
        :style="tab.selected ? gen_color(tab.color) : gen_color_outline(tab.color, 2)"
        v-on:click="select_tab(tab)"
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
      all_tab: {},
      tab_index: 0,
      show_all: false,
    }
  },
  mounted() {
    this.all_tab.color = getStyle('--primary') || '#304ffe'
    this.tabs = []
    this.get_data('environment', [], {'orderby': 'tree_order', 'asc': true}, this.get_data_response)
  },
  methods: {
    select_tab(tab, refresh = true) {
      tab.selected = !tab.selected
      if (tab.handler) {
        tab.handler(tab)
      }
      this.emitter.emit('environment_change_tab', this.tabs.filter(t => t.selected))
      this.check_all()
      //if (refresh) {
      //  this.refreshTable()
      //  this.add_history()
      //}
    },
    get_data_response(response) {
      if (response.data) {
        this.tabs = []
        var rows = response.data.data || []
        rows.forEach((row, index) => {
          row.index = index
          this.tabs.push(row)
        })
      }
      this.reload()
    },
    reload() {
      var env_indexes = decodeURIComponent(this.$route.query.env_indexes)
      if (env_indexes != undefined) {
        env_indexes = env_indexes.split(',')
        this.tabs.forEach(t => {
          t.selected = env_indexes.indexOf(t.index.toString()) >= 0
        })
      }
      this.check_all()
    },
    is_admin() {
      var permissions = localStorage.getItem('permissions') || []
      return permissions.includes('rw_all') || permissions.includes('rw_environment')
    },
    go_settings() {
      this.$router.push('/environment')
    },
    check_all() {
      this.show_all = this.tabs.every(t => t.selected)
    },
    select_all() {
      this.show_all = !this.show_all
      this.tabs.forEach(t => {
        t.selected = this.show_all
      })
      this.emitter.emit('environment_change_tab', this.tabs.filter(t => t.selected))
    },
  },
  watch: {
    $route() {
      this.$nextTick(this.reload);
    }
  },
}
</script>
