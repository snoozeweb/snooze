<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint="aggregate"
      order_by="timestamp"
      :is_ascending="false"
      @row-selected="select"
      :fields="fields"
      :tabs="tabs"
      :add_mode="false"
      :edit_mode="false"
      :delete_mode="false"
    >
      <template v-slot:cell(state)="row">
        <b-badge
          v-if="row.item.state !== undefined && row.item.state != null"
          :variant="get_state_color(row.item.state)"
        >
          {{ row.item.state }}
        </b-badge>
        <b-badge v-else variant="light">no state</b-badge>
      </template>
      <template #button="row">
        <b-button variant="success" v-if="can_be_reescalated(row.item)" @click="modal_reescalate([row.item])" size="sm">Re-escalate</b-button>
        <b-button variant="warning" v-if="can_be_acked(row.item)" @click="modal_ack([row.item])" size="sm">Acknowledge</b-button>
      </template>
      <template #selected_buttons>
        <b-button v-if="selection_reescalated.length > 0" variant="success" @click="modal_reescalate(selection_reescalated)" size="sm">Re-escalate ({{ selection_reescalated.length }})</b-button>
        <b-button v-if="selection_acked.length > 0" variant="warning" @click="modal_ack(selection_acked)" size="sm">Acknowledge ({{ selection_acked.length }})</b-button>
      </template>
    </List>

    <b-modal
      id="ack"
      ref="ack"
      @ok="acknowledge(modal_message, modal_data)"
      @hidden="modal_clear()"
      header-bg-variant="warning"
      size ="xl"
      centered
    >
      <template #modal-title>Acknowledge</template>
      {{ modal_data }}
      <b-form-group label="Acknowledgement message:">
        <b-form-input v-model="modal_message" />
      </b-form-group>
    </b-modal>

    <b-modal
      id="reescalate"
      ref="reescalate"
      @ok="reescalate(modal_message, modal_data)"
      @hidden="modal_clear()"
      header-bg-variant="success"
      size ="xl"
      centered
    >
      <template #modal-title>Re-escalate</template>
      {{ modal_data }}
      <b-form-group label="Re-escalation message:">
        <b-form-input v-model="modal_message" />
      </b-form-group>
    </b-modal>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import moment from 'moment'
import { update_items } from '@/utils/api'
import { form, fields } from '@/objects/Aggregate.yaml'

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
        {title: 'Alerts', filter: ['OR',
            ['NOT', ['EXISTS', 'state']],
            ['=', 'state', 'reescalated'],
          ],
        },
        {title: 'Acknowledged', filter: ['=', 'state', 'ack']},
        {title: 'Re-escalated', filter: ['=', 'state', 'reescalated']},
        {title: 'All', filter: []},
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
        var user = localStorage.getItem('name') || 'Unkwnown'
        item.acks.push({
          message: message,
          user: user,
          date: moment().format(),
        })
      })
      update_items("aggregate", items)
      this.$refs.table.refreshTable()
      this.modal_clear()
    },
    reescalate(message, items) {
      items.forEach(item => {
        if (item.escalations === undefined) {
          item.escalations = []
        }
        item.state = 'reescalated'
        var user = localStorage.getItem('name') || 'Unkwnown'
        item.escalations.push({
          message: message,
          user: user,
          date: moment().format(),
        })
        update_items("aggregate", items)
        this.$refs.table.refreshTable()
        this.modal_clear()
      })
    },
  },
}
</script>
