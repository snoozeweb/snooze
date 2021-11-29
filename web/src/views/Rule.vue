<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="rule"
      ref="list"
      edit_mode
      delete_mode
      add_mode
    >
      <template #custom_buttons="row">
        <CButton size="sm" @click="show_children(row.item)" color="info" v-c-tooltip="{content: 'Children'}"><i class="la la-folder-open la-lg"></i></CButton>
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
