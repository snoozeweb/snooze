<template>
  <div class="animated fadeIn">
    <List
      @update="assign_children"
      @select="select_propagate"
      @reload="reset_search"
      :tabs_prop="tabs_prop"
      :no_search="no_search"
      :no_paging="no_paging"
      :no_header="no_header"
      :no_history="no_history"
      endpoint_prop="rule"
      ref="list"
      edit_mode
      delete_mode
      add_mode
    >
      <template #custom_buttons="row">
        <CButton v-if="row.item['_children_count']" size="sm" @click="toggle_children(row.item, $event)" color="info"><i v-if="row.item['_showChildren']" class="la la-folder-open la-lg"></i><i v-else class="la la-folder la-lg"></i><CBadge v-if="row.item['_children_count']" color='light' class='position-absolute text-dark' style='z-index: 10; top:0!important; left:100%!important; transform:translate(-50%,-50%)!important'>{{ row.item['_children_count'] }}</CBadge></CButton>
        <CButton v-if="plus_button" size="sm" @click="add_child(row.item, $event)" color="success" v-c-tooltip="{content: 'Add child'}"><i class="la la-plus la-lg"></i></CButton>
        <CButton v-else size="sm" @click="show_details(row.item, $event)" color="warning" v-c-tooltip="{content: 'Show'}"><i class="la la-search-plus la-lg"></i></CButton>
      </template>
      <template #details_side="row">
        <AuditLogs collection="rule" :object="row.item" />
      </template>
      <template #details_footer="row">
        <div class="bg-light" v-if="Boolean(row.item._showChildren)">
          <Rule
            class="ps-3"
            @propagate_parent="propagate_parent"
            :tabs_prop="get_children(row.item)"
            :ref="row.item.uid"
            no_search
            no_header
            no_history
          />
        </div>
      </template>
    </List>
  </div>
</template>

<script>
import AuditLogs from '@/components/AuditLogs.vue'
import List from '@/components/List.vue'
import { get_data } from '@/utils/api'

export default {
  components: {
    AuditLogs,
    List,
  },
  emits: ['propagate_parent'],
  props: {
    tabs_prop: {
      type: Array,
      default: () => { return [] },
    },
    no_search: {type: Boolean, default: false},
    no_paging: {type: Boolean, default: false},
    no_header: {type: Boolean, default: false},
    no_history: {type: Boolean, default: false},
  },
  data () {
    return {
      plus_button: true,
    }
  },
  mounted () {
    this.$refs.list.select_all = () => {
      this.$refs.list.selected = []
      this.$refs.list.items.forEach(item => {
        item._selected = true
        this.$refs.list.selected.push(item)
        if (this.$refs[item.uid]) {
          this.$refs.list.selected = this.$refs.list.selected.concat(this.$refs[item.uid].$refs.list.select_all())
        }
      })
      //this.$emit('select_all')
      return this.$refs.list.selected
    }
    this.$refs.list.clear_selected = () => {
      var selected = this.$refs.list.selected
      this.$refs.list.items.forEach(item => {
        item._selected = false
        if (this.$refs[item.uid]) {
          selected = selected.concat(this.$refs[item.uid].$refs.list.clear_selected())
        }
      })
      this.$refs.list.selected = []
      //this.$emit('clear_selected')
      return selected
    }
    this.$refs.list.get_items_length = () => {
      var children_count = 0
      this.$refs.list.items.forEach(item => {
        if (item._showChildren && this.$refs[item.uid]) {
          children_count += this.$refs[item.uid].$refs.list.get_items_length()
        }
      })
      return this.$refs.list.items.length + children_count
    }
  },
  methods: {
    add_child (item) {
      this.$refs.list.modal_add(item)
    },
    select_propagate (item) {
      this.$emit('propagate_parent', [item], item._selected)
    },
    assign_children () {
      if (this.$refs.list.search_value) {
        return
      }
      this.$refs.list.items.forEach(item => {
        var query = ['=', 'parent', item.uid]
        var options = {
          perpage: 1,
          pagenb: 0,
        }
        get_data(this.$refs.list.endpoint, query, options, this.assign_children_feedback, item)
      })
    },
    assign_children_feedback (response, item) {
      if (response.data) {
        item._children_count = response.data.count
      }
    },
    usued_show_children (item) {
      var new_tab = {title: item.name + '>', filter: ['=', 'parent', item.uid], row: item, "parent": item.uid}
      this.$refs.list.tabs.splice(this.$refs.list.tab_index + 1)
      this.$refs.list.tabs.push(new_tab)
      this.$refs.list.changeTab(new_tab)
    },
    get_children (item) {
      return [{title: item.name + '>', filter: ['=', 'parent', item.uid], row: item, "parent": item.uid}]
    },
    toggle_children (item, event) {
      event.stopPropagation()
      item._showChildren = !item._showChildren
      if (!item._showChildren) {
        var selected = this.$refs[item.uid].$refs.list.clear_selected()
        this.$refs.list.selected = this.$refs.list.selected.filter(v => !selected.includes(v), 1)
        this.$emit('propagate_parent', selected, false)
      }
    },
    propagate_parent (selected, add_elements) {
      if (add_elements) {
        this.$refs.list.selected = this.$refs.list.selected.concat(selected)
      } else {
        this.$refs.list.selected = this.$refs.list.selected.filter(v => !selected.includes(v), 1)
      }
      this.$emit('propagate_parent', selected, add_elements)
    },
    reset_search () {
      var search = this.$refs.list.search_value || this.$route.query.s
      if (search) {
        this.$refs.list.tabs[0].filter = []
        this.plus_button = false
      } else {
        this.plus_button = true
        if (!this.$refs.list.tabs[0].parent) {
          this.$refs.list.tabs[0].filter = ['NOT', ['EXISTS', 'parent']]
        }
      }
    },
  },
}
</script>
