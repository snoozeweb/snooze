<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint_prop="alert"
      :info_excluded_fields="['smtp']"
      show_tabs
    >
      <template #custom_buttons="row">
        <CButton color="primary" class='text-nowrap' @click="modal_show([row.item], 'comment')" size="sm" v-c-tooltip="{content: 'Add comment'}"><i class="la la-comment-dots la-lg"></i> <CBadge v-if="row.item['comment_count']" color='light' class='position-absolute text-dark' style='z-index: 10; top:0!important; right:100%!important; transform:translate(50%,-50%)!important'>{{ row.item['comment_count'] }}</CBadge></CButton>
        <CButton class="btn-info" v-if="row.item.ttl >= 0" @click="toggle_ttl([row.item])" size="sm" v-c-tooltip="{content: 'Shelve'}"><i class="la la-folder-plus la-lg"></i></CButton>
        <CButton class="btn-info" v-else @click="toggle_ttl([row.item])" size="sm" v-c-tooltip="{content: 'Unshelve'}"><i class="la la-folder-minus la-lg"></i></CButton>
        <CButton class="btn-warning" v-if="can_be_reescalated(row.item)" @click="modal_show([row.item], 'esc')" size="sm" v-c-tooltip="{content: 'Re-escalate'}"><i class="la la-exclamation la-lg"></i></CButton>
        <CButton class="btn-success" v-if="can_be_acked(row.item)" @click="modal_show([row.item], 'ack')" size="sm" v-c-tooltip="{content: 'Acknowledge'}"><i class="la la-thumbs-up la-lg"></i></CButton>
        <CButton class="btn-tertiary" v-if="can_be_closed(row.item)" @click="modal_show([row.item], 'close')" size="sm" v-c-tooltip="{content: 'Close'}"><i class="la la-lock la-lg"></i></CButton>
        <CButton class="btn-quaternary" v-if="can_be_reopened(row.item)" @click="modal_show([row.item], 'open')" size="sm" v-c-tooltip="{content: 'Re-open'}"><i class="la la-lock-open la-lg"></i></CButton>
      </template>
      <template #selected_buttons>
        <CButton class="btn-primary" v-if="selection_comment.length > 0" @click="modal_show(selection_comment, 'comment')" size="sm">Comment ({{ selection_comment.length }})</CButton>
        <CButton class="btn-info" v-if="selection_shelved.length > 0" @click="toggle_ttl(selection_shelved)" size="sm">Shelve ({{ selection_shelved.length }})</CButton>
        <CButton class="btn-info" v-if="selection_unshelved.length > 0" @click="toggle_ttl(selection_unshelved)" size="sm">Unshelve ({{ selection_unshelved.length }})</CButton>
        <CButton class="btn-warning" v-if="selection_reescalated.length > 0" @click="modal_show(selection_reescalated, 'esc')" size="sm">Re-escalate ({{ selection_reescalated.length }})</CButton>
        <CButton class="btn-success" v-if="selection_acked.length > 0" @click="modal_show(selection_acked, 'ack')" size="sm">Acknowledge ({{ selection_acked.length }})</CButton>
        <CButton class="btn-tertiary" v-if="selection_closed.length > 0" @click="modal_show(selection_closed, 'close')" size="sm">Close ({{ selection_closed.length }})</CButton>
        <CButton class="btn-quaternary" v-if="selection_reopened.length > 0" @click="modal_show(selection_reopened, 'open')" size="sm">Open ({{ selection_reopened.length }})</CButton>
      </template>
      <template #info="row">
        <Mail :smtp="row.item.smtp" v-if="!!row.item.smtp" class="pb-2"/>
        <Grafana :data="row.item" v-if="!!row.item.image_url" class="pb-2"/>
        <Prometheus :data="row.item.prometheus" v-if="!!row.item.prometheus" class="pb-2"/>
      </template>
      <template #details_side="row">
        <Timeline :record="row.item" ref="timeline" v-if="row.item['comment_count']" />
      </template>
      <template #head_buttons>
        <CButton v-if="is_admin()" color="success" @click="modal_add()">New</CButton>
        <CTooltip :content="auto_refresh ? 'Auto Refresh ON':'Auto Refresh OFF'" trigger="hover">
          <template #toggler="{ on }">
            <CButton size="sm" :color="auto_refresh ? 'success':'secondary'" @click="toggle_auto" v-on="on">
              <i v-if="auto_refresh" class="la la-eye la-lg"></i>
              <i v-else class="la la-eye-slash la-lg"></i>
            </CButton>
          </template>
        </CTooltip>
      </template>
    </List>

  <CModal
    ref="modal"
    :visible="show_modal"
    @close="modal_clear"
    alignment="center"
    size="xl"
    backdrop="static"
  >
    <CModalHeader :class="`bg-${modal_bg_variant}`">
      <CModalTitle :class="`text-${modal_text_variant}`">{{ modal_title }}</CModalTitle>
    </CModalHeader>
    <CModalBody>
      <CFormFloating>
        <CFormInput id="floatingInput" v-model="modal_message" placeholder="message"/>
        <CFormLabel for="floatingInput">Message (optional)</CFormLabel>
      </CFormFloating>
      <CRow v-if="modal_type == 'esc' || modal_type == 'open'" class="mt-3">
        <CCol col=3 md=2>
          <CPopover
            content="Apply modifications to the record then notify"
            :trigger="['hover', 'focus']"
            placement="right"
          >
          </CPopover>
          <CTooltip content="Apply modifications to the record then notify" placement="right" trigger="hover">
            <template #toggler="{ on }">
              <label id="title_modifications" v-on="on">Modifications (optional):</label>
            </template>
          </CTooltip>
        </CCol>
        <CCol col=9 md=10>
          <Modification
            v-model="modifications"
          />
        </CCol>
      </CRow>
    </CModalBody>
    <CModalFooter>
      <CButton @click="modal_clear" color="secondary">Cancel</CButton>
      <CButton @click="add_comment(modal_message, modal_data, modal_type, modifications)" :color="modal_bg_variant">OK</CButton>
    </CModalFooter>
  </CModal>
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
import Prometheus from '@/components/info/Prometheus.vue'

export default {
  components: {
    List,
    Timeline,
    Modification,
    Mail,
    Grafana,
    Prometheus,
  },
  mounted () {
    if(localStorage.getItem('record_auto') == 'true') {
      this.auto_refresh = true
    }
    this.auto_update()
    this.$refs.table.submit_add_back = this.$refs.table.submit_add
    this.$refs.table.submit_add = this.submit_add
  },
  data () {
    return {
      show_modal: false,
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
      return this.$refs.table.selected.filter(item => item.ttl >= 0)
    },
    selection_unshelved: function() {
      return this.$refs.table.selected.filter(item => item.ttl == undefined || item.ttl < 0)
    },
    selection_reescalated: function() {
      return this.$refs.table.selected.filter(item => this.can_be_reescalated(item))
    },
    selection_acked: function() {
      return this.$refs.table.selected.filter(item => this.can_be_acked(item))
    },
    selection_comment: function() {
      return this.$refs.table.selected
    },
    selection_closed: function() {
      return this.$refs.table.selected.filter(item => this.can_be_closed(item))
    },
    selection_reopened: function() {
      return this.$refs.table.selected.filter(item => this.can_be_reopened(item))
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
      this.show_modal = false
      Array.from(document.getElementsByClassName('modal')).forEach(el => el.style.display = "none")
      Array.from(document.getElementsByClassName('modal-backdrop')).forEach(el => el.style.display = "none")
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
      this.show_modal = true
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
      this.$refs.table.set_busy(true)
      add_items("comment_self", comments, this.callback, {'items': items, 'type': type})
      this.modal_clear()
    },
    callback(response, arg) {
      this.$refs.table.refreshTable()
    },
    toggle_auto() {
      this.auto_refresh = !this.auto_refresh
      this.auto_update()
    },
    auto_update() {
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
