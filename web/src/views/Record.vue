<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint="record"
      order_by="timestamp"
      :is_ascending="false"
      @row-selected="select"
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
      ],
    }
  },
  computed: {
    selection_reescalated: function() {
      return this.selected.filter(item => this.can_be_reescalated(item))
    },
    selection_acked: function() {
      return this.selected.filter(item => this.can_be_acked(item))
    },
  },
  methods: {
    get_state_color(state) {
      switch(state) {
        case 'ack':
          return 'warning'
        case 'snoozed':
          return 'secondary'
        case 'reescalated':
          return 'success'
        default:
          return 'light'
      }
    },
    can_be_reescalated(item) {
      return ['ack', 'snoozed'].includes(item.state)
    },
    can_be_acked(item) {
      return [null, undefined, 'reescalated'].includes(item.state)
    },
    modal_clear() {
      this.modal_data = []
      this.modal_message = null
    },
    modal_ack(items) {
      this.modal_data = items
      this.$bvModal.show('ack')
    },
    modal_reescalate(items) {
      this.modal_data = items
      this.$bvModal.show('reescalate')
    },
    select(items) {
      this.selected = items
    },
    acknowledge(message, items) {
      items.forEach(item => {
        if (item.acks === undefined) {
          item.acks = []
        }
        item.state = 'ack'
        var user = 'TODO'
        var now = new Date()
        item.acks.push({
          message: message,
          user: user,
          date: now.toISOString(),
        })
      })
      update_items("record", items)
      this.$refs.table.refreshTable()
      this.modal_clear()
    },
    reescalate(message, items) {
      items.forEach(item => {
        if (item.escalations === undefined) {
          item.escalations = []
        }
        item.state = 'reescalated'
        var user = 'TODO'
        var now = new Date()
        item.escalations.push({
          message: message,
          user: user,
          date: now.toISOString(),
        })
        update_items("record", items)
        this.$refs.table.refreshTable()
        this.modal_clear()
      })
    },
  },
}
</script>
