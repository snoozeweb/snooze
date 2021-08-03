<template>
  <div class="animated fadeIn">
    <List
      endpoint="rule"
      order_by="name"
      is_ascending
      :form="form"
      :fields="fields"
      :tabs="tabs"
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

import { form, fields } from '@/objects/Rule.yaml'

export default {
  components: {
    List,
  },
  data () {
    return {
      form: form,
      fields: fields,
      tabs: [
        {title: 'Rules', filter: ['NOT', ['EXISTS', 'parent']]},
      ],
    }
  },
  methods: {
    show_children(item) {
      var new_tab = {title: item.name + '>', filter: ['=', 'parent', item.uid], row: item, "parent": item.uid}
      this.$refs.list.tabs.splice(this.$refs.list.tab_index + 1)
      this.tabs.push(new_tab)
      this.$refs.list.changeTab(new_tab)
    },
  },
}
</script>
