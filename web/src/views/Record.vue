<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint="record"
      order_by="timestamp"
      :is_ascending="false"
      :fields="fields"
      :tabs="tabs"
      :add_mode="false"
      :edit_mode="false"
      :delete_mode="false"
    >
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { update_items } from '@/utils/api'
import { form, fields } from '@/objects/Record.yaml'

export default {
  components: {
    List,
  },
  mounted () {
  },
  data () {
    return {
      selected: [],
      modal_message: null,
      modal_data: [],
      form: form,
      fields: fields,
      tabs: [
        {title: 'All Records', filter: []},
        {title: 'Snoozed', filter: ['EXISTS', 'snoozed']}
      ],
    }
  },
  computed: {
  },
  methods: {
  },
}
</script>
