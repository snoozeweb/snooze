<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="rule"
      order_by="name"
      is_ascending
      ref="list"
      edit_mode
      delete_mode
      add_mode
    >
      <template #button="row">
        <b-button size="sm" @click="show_children(row.item)" variant="info" v-b-tooltip.hover title="Children"><i class="la la-folder-open la-lg"/></b-button>
      </template>
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

export default {
  components: {
    List,
  },
  methods: {
    show_children(item) {
      var new_tab = {title: item.name + '>', filter: ['=', 'parent', item.uid], row: item, "parent": item.uid}
      this.$refs.list.tabs.splice(this.$refs.list.tab_index + 1)
      this.$refs.list.tabs.push(new_tab)
      this.$refs.list.changeTab(new_tab)
    },
  },
}
</script>
