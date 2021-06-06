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
      <template #button="row">
        <b-button variant="info" v-if="row.item.ttl >= 0" @click="toggle_ttl([row.item])" size="sm" v-b-tooltip.hover title="Shelve"><i class="la la-folder-plus la-lg"/></b-button>
        <b-button variant="info" v-else @click="toggle_ttl([row.item])" size="sm" v-b-tooltip.hover title="Unshelve"><i class="la la-folder-minus la-lg"/></b-button>
        <b-button variant="warning" v-if="can_be_reescalated(row.item)" @click="modal_show([row.item], 'reescalate')" size="sm" v-b-tooltip.hover title="Re-escalate"><i class="la la-exclamation la-lg"/></b-button>
        <b-button variant="success" v-if="can_be_acked(row.item)" @click="modal_show([row.item], 'ack')" size="sm" v-b-tooltip.hover title="Acknowledge"><i class="la la-thumbs-up la-lg"/></b-button>
        <b-button variant="primary" class='text-nowrap' @click="modal_show([row.item], 'comment')" size="sm" v-b-tooltip.hover title="Add comment"><i class="las la-comment-dots la-lg"/> <b-badge v-if="row.item['timeline']" variant='light' class='mfs-auto'>{{ row.item['timeline'].length }}</b-badge></b-button>
      </template>
      <template #selected_buttons>
        <b-button v-if="selection_shelved.length > 0" variant="info" @click="toggle_ttl(selection_shelved)" size="sm">Shelve ({{ selection_shelved.length }})</b-button>
        <b-button v-if="selection_unshelved.length > 0" variant="primary" @click="toggle_ttl(selection_unshelved)" size="sm">Unshelve ({{ selection_unshelved.length }})</b-button>
        <b-button v-if="selection_reescalated.length > 0" variant="warning" @click="modal_show(selection_reescalated, 'reescalate')" size="sm">Re-escalate ({{ selection_reescalated.length }})</b-button>
        <b-button v-if="selection_acked.length > 0" variant="success" @click="modal_show(selection_acked, 'ack')" size="sm">Acknowledge ({{ selection_acked.length }})</b-button>
        <b-button v-if="selection_comment.length > 0" variant="primary" @click="modal_show(selection_comment, 'comment')" size="sm">Comment ({{ selection_comment.length }})</b-button>
      </template>
    </List>

    <b-modal
      id="ack"
      ref="ack"
      @ok="write_timeline(modal_message, modal_data, 'ack')"
      @hidden="modal_clear()"
      header-bg-variant="success"
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
      @ok="write_timeline(modal_message, modal_data, 'reescalated')"
      @hidden="modal_clear()"
      header-bg-variant="warning"
      size ="xl"
      centered
    >
      <template #modal-title>Re-escalate</template>
      {{ modal_data }}
      <b-form-group label="Re-escalation message:">
        <b-form-input v-model="modal_message" />
      </b-form-group>
    </b-modal>

    <b-modal
      id="comment"
      ref="comment"
      @ok="write_timeline(modal_message, modal_data, 'comment')"
      @hidden="modal_clear()"
      header-bg-variant="primary"
      size ="xl"
      centered
    >
      <template #modal-title>Add a comment</template>
      {{ modal_data }}
      <b-form-group label="Comment:">
        <b-form-input v-model="modal_message" />
      </b-form-group>
    </b-modal>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import moment from 'moment'
import { update_items, preprocess_data } from '@/utils/api'
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
        {title: 'Alerts', filter: ['AND', 
            ['OR',
              ['NOT', ['EXISTS', 'state']],
              ['=', 'state', 'reescalated'],
            ],
            ['AND',
              ['NOT', ['EXISTS', 'snooze']],
              ['>=', 'ttl', 0],
           ],
          ]
        },
        {title: 'Snoozed', filter: ['EXISTS', 'snoozed']},
        {title: 'Acknowledged', filter: ['=', 'state', 'ack']},
        {title: 'Re-escalated', filter: ['=', 'state', 'reescalated']},
        {title: 'Shelved', filter: ['OR',
            ['NOT', ['EXISTS', 'ttl']],
            ['<', 'ttl', 0],
          ]
        },
        {title: 'All', filter: []},
      ],
    }
  },
  computed: {
    selection_shelved: function() {
      return this.selected.filter(item => item.ttl >= 0)
    },
    selection_unshelved: function() {
      return this.selected.filter(item => item.ttl == undefined || item.ttl < 0)
    },
    selection_reescalated: function() {
      return this.selected.filter(item => this.can_be_reescalated(item))
    },
    selection_acked: function() {
      return this.selected.filter(item => this.can_be_acked(item))
    },
    selection_comment: function() {
      return this.selected
    },
  },
  methods: {
    can_be_shelved(item) {
      return item.ttl >= 0
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
    modal_show(items, type) {
      this.modal_data = items
      this.$bvModal.show(type)
    },
    select(items) {
      this.selected = items
    },
    toggle_ttl(items, ttl) {
      items.forEach(item => {
        if (item.ttl === undefined) {
          item.ttl = 0
        } else {
          item.ttl = item.ttl * -1
        }
        item.date_epoch = moment().unix()
      })
      update_items("record", items, this.callback)
    },
    write_timeline(message, items, type) {
      items.forEach(item => {
        if (item.timeline === undefined) {
          item.timeline = []
        }
        if(type != 'comment') {
          item.state = type
        }
        var user = {'name': localStorage.getItem('name') || '', 'method': localStorage.getItem('method')}
        item.timeline.push({
          message: message,
          user: user,
          type: type,
          date: moment().format(),
        })
        update_items("record", items, this.callback)
        this.modal_clear()
      })
    },
    callback(response) {
      this.$refs.table.refreshTable()
      var title, message, variant
      if (response.data) {
        title = 'Success!'
        variant = 'success'
        message = 'The operation was successful'
      } else {
        title = 'Error'
        message = 'The operation could not be completed'
        variant = 'danger'
      }
      this.$bvToast.toast(message, {
        title: title,
        variant: variant,
        solid: true,
      })
    },
  },
}
</script>
