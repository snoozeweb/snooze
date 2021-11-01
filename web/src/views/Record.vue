<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint_prop="alert"
      @row-selected="select"
      :info_excluded_fields="['smtp']"
    >
      <template #button="row">
        <b-button variant="primary" class='text-nowrap' @click="modal_show([row.item], 'comment')" size="sm" v-b-tooltip.hover title="Add comment"><i class="las la-comment-dots la-lg"></i> <b-badge v-if="row.item['comment_count']" variant='light' class='position-absolute' style='z-index: 10; top:0!important; right:100%!important; transform:translate(50%,-50%)!important'>{{ row.item['comment_count'] }}</b-badge></b-button>
        <b-button variant="info" v-if="row.item.ttl >= 0" @click="toggle_ttl([row.item])" size="sm" v-b-tooltip.hover title="Shelve"><i class="la la-folder-plus la-lg"></i></b-button>
        <b-button variant="info" v-else @click="toggle_ttl([row.item])" size="sm" v-b-tooltip.hover title="Unshelve"><i class="la la-folder-minus la-lg"></i></b-button>
        <b-button variant="warning" v-if="can_be_reescalated(row.item)" @click="modal_show([row.item], 'esc')" size="sm" v-b-tooltip.hover title="Re-escalate"><i class="la la-exclamation la-lg"></i></b-button>
        <b-button variant="success" v-if="can_be_acked(row.item)" @click="modal_show([row.item], 'ack')" size="sm" v-b-tooltip.hover title="Acknowledge"><i class="la la-thumbs-up la-lg"></i></b-button>
        <b-button variant="tertiary" v-if="can_be_closed(row.item)" @click="modal_show([row.item], 'close')" size="sm" v-b-tooltip.hover title="Close"><i class="la la-lock la-lg"></i></b-button>
        <b-button variant="quaternary" v-if="can_be_reopened(row.item)" @click="modal_show([row.item], 'open')" size="sm" v-b-tooltip.hover title="Re-open"><i class="la la-lock-open la-lg"></i></b-button>
      </template>
      <template #selected_buttons>
        <b-button v-if="selection_comment.length > 0" variant="primary" @click="modal_show(selection_comment, 'comment')" size="sm">Comment ({{ selection_comment.length }})</b-button>
        <b-button v-if="selection_shelved.length > 0" variant="info" @click="toggle_ttl(selection_shelved)" size="sm">Shelve ({{ selection_shelved.length }})</b-button>
        <b-button v-if="selection_unshelved.length > 0" variant="info" @click="toggle_ttl(selection_unshelved)" size="sm">Unshelve ({{ selection_unshelved.length }})</b-button>
        <b-button v-if="selection_reescalated.length > 0" variant="warning" @click="modal_show(selection_reescalated, 'esc')" size="sm">Re-escalate ({{ selection_reescalated.length }})</b-button>
        <b-button v-if="selection_acked.length > 0" variant="success" @click="modal_show(selection_acked, 'ack')" size="sm">Acknowledge ({{ selection_acked.length }})</b-button>
        <b-button v-if="selection_closed.length > 0" variant="tertiary" @click="modal_show(selection_closed, 'close')" size="sm">Close ({{ selection_closed.length }})</b-button>
        <b-button v-if="selection_reopened.length > 0" variant="quaternary" @click="modal_show(selection_reopened, 'open')" size="sm">Open ({{ selection_reopened.length }})</b-button>
      </template>
      <template #info="row">
        <Mail :smtp="row.item.smtp" v-if="!!row.item.smtp" />
        <Grafana :data="row.item" v-if="!!row.item.image_url" />
      </template>
      <template #details_side="row">
        <b-col v-if="row.item['comment_count']">
          <b-card header='Timeline' header-class='text-center font-weight-bold' body-class='p-2'>
            <Timeline :record="row.item" ref="timeline"/>
          </b-card>
        </b-col>
      </template>
      <template #head_buttons>
        <b-button v-if="is_admin()" variant="success" @click="modal_add()">New</b-button>
        <b-button size="sm" :variant="auto_refresh ? 'success':''" v-b-tooltip.hover :title="auto_refresh ? 'Auto Mode ON':'Auto Mode OFF'" @click="toggle_auto()" :pressed.sync="auto_refresh"><i v-if="auto_refresh" class="la la-eye la-lg"/><i v-else="auto_refresh" class="la la-eye-slash la-lg"/></b-button>
      </template>
    </List>

    <b-modal
      id="modal"
      ref="modal"
      @ok="add_comment(modal_message, modal_data, modal_type, modifications)"
      @hidden="modal_clear()"
      :header-bg-variant="modal_bg_variant"
      :header-text-variant="modal_text_variant"
      size="xl"
      centered
    >
      <template #modal-title>{{ modal_title }}</template>
      <b-form-group label="Message (optional):">
        <b-form-input v-model="modal_message" />
      </b-form-group>
      <b-row v-if="modal_type == 'esc'">
        <b-col cols=3 md=2>
          <label id="title_modifications" >Modifications (optional):</label>
          <b-popover
            target="title_modifications"
            content="Apply modifications to the record then notify"
            triggers="hover focus"
            placement="right"
          ></b-popover>
        </b-col>
        <b-col cols=9 md=10>
          <Modification
            v-model="modifications"
          />
        </b-col>
      </b-row>
    </b-modal>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import moment from 'moment'
import { add_items, update_items } from '@/utils/api'
import Timeline from '@/components/Timeline.vue'
import Modification from '@/components/form/Modification.vue'

import Mail from '@/components/info/Mail.vue'
import Grafana from '@/components/info/Grafana.vue'

export default {
  components: {
    List,
    Timeline,
    Modification,
    Mail,
    Grafana,
  },
  mounted () {
    if(localStorage.getItem('record_auto') == 'true') {
      this.auto_refresh = true
    }
    this.toggle_auto()
    this.$refs.table.submit_add_back = this.$refs.table.submit_add
    this.$refs.table.submit_add = this.submit_add
  },
  data () {
    return {
      selected: [],
      modal_title: '',
      modal_message: null,
      modal_type: '',
      modal_bg_variant: '',
      modal_text_variant: '',
      modal_data: [],
      modifications: [],
      auto_refresh: false,
      auto_interval: null,
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
    selection_closed: function() {
      return this.selected.filter(item => this.can_be_closed(item))
    },
    selection_reopened: function() {
      return this.selected.filter(item => this.can_be_reopened(item))
    },
  },
  methods: {
    can_be_shelved(item) {
      return item.ttl >= 0
    },
    can_be_reescalated(item) {
      return ['ack', 'snoozed'].includes(item.state) && this.can_be_closed(item)
    },
    can_be_acked(item) {
      return [null, undefined, 'esc', 'open', ''].includes(item.state) && this.can_be_closed(item)
    },
    can_be_closed(item) {
      return !['close'].includes(item.state)
    },
    can_be_reopened(item) {
      return ['close'].includes(item.state)
    },
    modal_clear() {
      this.modal_data = []
      this.modal_title = ''
      this.modal_message = null
      this.modal_type = ''
      this.modal_bg_variant = ''
      this.modal_text_variant = ''
    },
    modal_show(items, type) {
      this.modal_data = items
      this.modal_type = type
      this.modifications = []
      switch (type) {
        case 'ack':
          this.modal_title = 'Acknowledge'
          this.modal_bg_variant = 'success'
          this.modal_text_variant = 'white'
          break
        case 'esc':
          this.modal_title = 'Re-escalate'
          this.modal_bg_variant = 'warning'
          this.modal_text_variant = ''
          break
        case 'close':
          this.modal_title = 'Close'
          this.modal_bg_variant = 'tertiary'
          this.modal_text_variant = 'white'
          break
        case 'open':
          this.modal_title = 'Re-open'
          this.modal_bg_variant = 'quaternary'
          this.modal_text_variant = ''
          break
        default:
          this.modal_title = 'Add a comment'
          this.modal_bg_variant = 'primary'
          this.modal_text_variant = 'white'
      }
      this.$bvModal.show('modal')
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
    add_comment(message, items, type, modifs = []) {
      //var user = {'name': localStorage.getItem('name') || '', 'method': localStorage.getItem('method')}
      var comments = []
      items.forEach(item => {
        comments.push({
          record_uid: item['uid'],
          type: type,
          message: message,
          date: moment().format(),
          modifications: modifs,
        })
      })
      add_items("comment_self", comments, this.callback, {'items': items, 'type': type})
      this.modal_clear()
    },
    callback(response, arg) {
      this.$refs.table.refreshTable()
    },
    toggle_auto() {
      if (this.auto_refresh) {
        this.auto_interval = setInterval(this.$refs.table.refreshTable, 10000)
      } else {
        if (this.auto_interval) {
          clearInterval(this.auto_interval)
          this.auto_interval = null
        }
      }
      localStorage.setItem('record_auto', this.auto_refresh)
    },
    is_admin() {
      var permissions = localStorage.getItem('permissions') || []
      return permissions.includes('rw_all') || permissions.includes('rw_record')
    },
    modal_add() {
      this.$refs.table.modal_add()
    },
    submit_add(bvModalEvt) {
      if (this.$refs.table.modal_data.add.custom_fields !== undefined) {
        this.$refs.table.modal_data.add.custom_fields.forEach(field => {
          if (field[0] != '') {
            this.$refs.table.modal_data.add[field[0]] = field[1]
          }
        })
      }
      this.$refs.table.submit_add_back(bvModalEvt, 'alert')
    },
  },
}
</script>
