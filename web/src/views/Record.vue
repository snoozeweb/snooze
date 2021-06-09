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
        <b-button variant="primary" class='text-nowrap' @click="modal_show([row.item], 'comment')" size="sm" v-b-tooltip.hover title="Add comment"><i class="las la-comment-dots la-lg"/> <b-badge v-if="row.item['comment_count']" variant='light' class='position-absolute' style='top:0!important; left:100%!important; transform:translate(-50%,-50%)!important'>{{ row.item['comment_count'] }}</b-badge></b-button>
      </template>
      <template #selected_buttons>
        <b-button v-if="selection_shelved.length > 0" variant="info" @click="toggle_ttl(selection_shelved)" size="sm">Shelve ({{ selection_shelved.length }})</b-button>
        <b-button v-if="selection_unshelved.length > 0" variant="primary" @click="toggle_ttl(selection_unshelved)" size="sm">Unshelve ({{ selection_unshelved.length }})</b-button>
        <b-button v-if="selection_reescalated.length > 0" variant="warning" @click="modal_show(selection_reescalated, 'reescalate')" size="sm">Re-escalate ({{ selection_reescalated.length }})</b-button>
        <b-button v-if="selection_acked.length > 0" variant="success" @click="modal_show(selection_acked, 'ack')" size="sm">Acknowledge ({{ selection_acked.length }})</b-button>
        <b-button v-if="selection_comment.length > 0" variant="primary" @click="modal_show(selection_comment, 'comment')" size="sm">Comment ({{ selection_comment.length }})</b-button>
      </template>
      <template #details_side="row">
        <b-col v-if="row.item['comment_count']">
          <b-card header='Timeline' header-class='text-center font-weight-bold' body-class='p-2'>
            <Timeline :record="row.item" ref="timeline"/>
          </b-card>
        </b-col>
      </template>
    </List>

    <b-modal
      id="ack"
      ref="ack"
      @ok="add_comment(modal_message, modal_data, 'ack')"
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
      @ok="add_comment(modal_message, modal_data, 'esc')"
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
      @ok="add_comment(modal_message, modal_data, 'comment')"
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
import { add_items, update_items } from '@/utils/api'
import { form, fields } from '@/objects/Record.yaml'
import Timeline from '@/components/Timeline.vue'

export default {
  components: {
    List,
    Timeline,
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
              ['OR',
                ['NOT', ['EXISTS', 'state']],
                ['=', 'state', ''],
              ],
              ['=', 'state', 'esc'],
            ],
            ['AND',
              ['NOT', ['EXISTS', 'snooze']],
              ['>=', 'ttl', 0],
           ],
          ]
        },
        {title: 'Snoozed', filter: ['EXISTS', 'snoozed']},
        {title: 'Acknowledged', filter: ['=', 'state', 'ack']},
        {title: 'Re-escalated', filter: ['=', 'state', 'esc']},
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
      return [null, undefined, 'esc', ''].includes(item.state)
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
    add_comment(message, items, type) {
      //var user = {'name': localStorage.getItem('name') || '', 'method': localStorage.getItem('method')}
      var comments = []
      items.forEach(item => {
        comments.push({
          record_uid: item['uid'],
	  type: type,
          message: message,
          //user: user,
          date: moment().format(),
        })
      })
      add_items("comment_self", comments, this.callback, {'items': items, 'type': type})
      this.modal_clear()
    },
    callback(response, arg) {
      this.$refs.table.refreshTable()
    },
  },
}
</script>
