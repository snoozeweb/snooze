<template>
  <CCol v-if="record['comment_count']" class="p-2">
    <CCard>
      <CCardHeader class='card-header-border text-center' style='font-weight:bold'>Timeline</CCardHeader>
      <CCardBody class="p-2">
        <div v-if="data != ''">
          <CRow v-for="row in data.slice().reverse()" :key="row['uid']" class="m-0">
            <CCol class="p-0">
              <CCard no-body class='mb-2'>
                <div class='position-absolute' style='top:0!important; right:0!important; margin-top: -1px; margin-right: -1px'>
                  <CButton v-if="can_edit(row)" size="sm" class='py-0_5 px-1' style='border-radius: 0 0 0 .25rem' color="primary" @click="modal_edit(row)" v-c-tooltip="{content: 'Edit'}"><i class="la la-pencil-alt la-lg"></i></CButton>
                  <CButton v-if="can_delete(row)" size="sm" class='py-0_5 px-1' style='border-radius: 0 .25rem 0 0' color="danger" @click="modal_delete(row)" v-c-tooltip="{content: 'Delete'}"><i class="la la-trash la-lg"></i></CButton>
                </div>
                <CCardBody class="d-flex p-2 align-items-center">
                  <CTooltip :content="get_alert_tooltip(row['type'])" trigger="hover">
                    <template #toggler="{ on }">
                      <div :class="'bg-' + get_alert_color(row['type']) + ' me-3 text-white rounded p-2'" :v-c-tooltip="'{content: ' + get_alert_tooltip(row['type']) + '}'" v-on="on">
                        <i :class="'la ' + get_alert_icon(row['type']) + ' la-2x'"></i>
                      </div>
                    </template>
                  </CTooltip>
                  <div>
                    <div>
                      <span class="fw-bold" style="font-size: 1.0rem">{{ row['name'] }}</span>
                      <span class="fst-italic muted"> @<DateTime :date="row['date']" show_secs /></span>
                      <span class="text-muted" style="font-size: 0.75rem" v-if="row['edited']"> (edited)</span>
                    </div>
                    <div class="text-muted">
                      {{ row['message'] }}
                    </div>
                    <div class="text-muted" v-if="row['modifications'] && row['modifications'].length > 0">
                      <CBadge color="warning">modifications</CBadge> <Modification style="font-size: 0.70rem;" :data="row['modifications']"/>
                    </div>
                  </div>
                </CCardBody>
              </CCard>
            </CCol>
          </CRow>
          <CFormTextarea
            id="textarea"
            v-model="input_text"
            placeholder="Add a comment"
            rows="1"
            max-rows="8"
            class='mb-2'
          ></CFormTextarea>
          <div>
            <CButtonToolbar role="group">
              <CButton size="sm" color='primary' @click="add_comment(input_text, 'comment')" v-c-tooltip="{content: 'Add a comment'}">Comment</CButton>
              <div class="d-flex ms-auto me-2 align-items-center">
                <div class="me-2">
                  <SPagination
                    v-model:activePage="current_page"
                    :pages="Math.ceil(nb_rows / per_page)"
                    ulClass="m-0"
                  />
                </div>
                <div>
                  <CRow class="align-items-center gx-0">
                    <CCol xs="auto px-1">
                      <CFormLabel for="perPageSelect" class="col-form-label col-form-label-sm">Per page</CFormLabel>
                    </CCol>
                    <CCol xs="auto px-1">
                      <CFormSelect
                        v-model="per_page"
                        id="perPageSelect"
                        size="sm"
                      >
                        <option v-for="opts in page_options" :value="opts">{{ opts }}</option>
                      </CFormSelect>
                    </CCol>
                  </CRow>
                </div>
              </div>
              <CButtonGroup role="group">
                <CTooltip :content="auto_mode ? 'Auto Refresh ON':'Auto Refresh OFF'" trigger="hover">
                  <template #toggler="{ on }">
                    <CButton size="sm" :color="auto_mode ? 'success':'secondary'" @click="toggle_auto" v-on="on">
                      <i v-if="auto_mode" class="la la-eye la-lg"></i>
                      <i v-else class="la la-eye-slash la-lg"></i>
                    </CButton>
                  </template>
                </CTooltip>
                <CButton size="sm" color="secondary" @click="refresh()" v-c-tooltip="{content: 'Refresh'}"><i class="la la-refresh la-lg"></i></CButton>
              </CButtonGroup>
            </CButtonToolbar>
          </div>
        </div>
      </CCardBody>
    </CCard>
    <CModal
      ref="timeline_edit"
      :visible="show_edit"
      @close="modal_clear"
      alignment="center"
      size ="xl"
      backdrop="static"
    >
      <CModalHeader class="bg-primary">
        <CModalTitle class="text-white">Edit</CModalTitle>
      </CModalHeader>
      <CModalBody>
        <CFormFloating>
          <CFormInput id="floatingInput" v-model="modal_data.edit.message" placeholder="message"/>
          <CFormLabel for="floatingInput">Message (optional)</CFormLabel>
        </CFormFloating>
      </CModalBody>
      <CModalFooter>
        <CButton @click="modal_clear" color="secondary">Cancel</CButton>
        <CButton @click="submit_edit" color="primary">OK</CButton>
      </CModalFooter>
    </CModal>

    <CModal
      ref="timeline_delete"
      :visible="show_delete"
      @close="modal_clear"
      alignment="center"
      size="xl"
      backdrop="static"
    >
      <CModalHeader class="bg-danger">
        <CModalTitle class="text-white">Delete item</CModalTitle>
      </CModalHeader>
      <CModalBody>
        <p>{{ modal_data.delete }}</p>
      </CModalBody>
      <CModalFooter>
        <CButton @click="modal_clear" color="secondary">Cancel</CButton>
        <CButton @click="submit_delete" color="danger">OK</CButton>
      </CModalFooter>
    </CModal>

  </CCol>
</template>

<script>
import DateTime from '@/components/DateTime.vue'
import Modification from '@/components/Modification.vue'
import SPagination from '@/components/SPagination.vue'
import moment from 'moment'
import { add_items, update_items, delete_items } from '@/utils/api'
import { get_data, get_alert_icon, get_alert_color, get_alert_tooltip } from '@/utils/api'
import { API } from '@/api'

export default {
  name: 'Timeline',
  components: {
    DateTime,
    Modification,
    SPagination,
  },
  props: {
    record: {type: Object},
  },
  data() {
    return {
      record_data: this.record,
      add_items: add_items,
      update_items: update_items,
      get_data: get_data,
      get_alert_icon: get_alert_icon,
      get_alert_color: get_alert_color,
      get_alert_tooltip: get_alert_tooltip,
      data: [],
      input_text: '',
      auto_mode: true,
      auto_interval: {},
      per_page: '5',
      page_options: ['5', '10', '20'],
      nb_rows: 0,
      current_page: 1,
      orderby: 'date',
      isascending: false,
      show_edit: false,
      show_delete: false,
      modal_data: {
        edit: {},
        delete: {},
      },
    }
  },
  mounted () {
    this.refresh()
    this.auto_interval = setInterval(this.refresh, 2000);
  },
  beforeUnmount () {
    if (this.auto_interval) {
      clearInterval(this.auto_interval)
    }
  },
  methods: {
    refresh () {
      //console.log(`GET /comment/['=','record_uid','${this.record_data.uid}']`)
      var query = ['=', 'record_uid', this.record_data.uid]
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.orderby !== undefined) { options["orderby"] = this.orderby }
      this.get_data('comment', query, options, this.update_data)
    },
    update_data(response) {
      if (response.data) {
          this.data = response.data.data
          this.record_data.comment_count = response.data.count
          this.nb_rows = response.data.count
          this.update_record()
      }
    },
    update_record() {
      this.get_data('record', ['=', 'uid', this.record_data.uid], null, this.update_record_callback)
    },
    update_record_callback(response) {
      if (response.data) {
        this.record_data.state = response.data.data[0].state
      }
    },
    add_comment(message, type) {
      var comment = {
        record_uid: this.record_data.uid,
        type: type,
        message: message,
        date: moment().format(),
      }
      add_items("comment_self", [comment], this.callback)
    },
    callback(response) {
      this.refresh()
      this.input_text = ''
    },
    is_admin() {
      var permissions = localStorage.getItem('permissions') || []
      return permissions.includes('rw_all') || permissions.includes('rw_comment')
    },
    can_comment(row) {
      var permissions = localStorage.getItem('permissions') || []
      var name = localStorage.getItem('name')
      var method = localStorage.getItem('method')
      return permissions.includes('can_comment') && name == row['name'] && method == row['method']
    },
    can_delete(row) {
      return this.is_admin() || this.can_comment(row)
    },
    can_edit(row) {
      return this.can_delete(row)
    },
    modal_edit (item) {
      var new_item = JSON.parse(JSON.stringify(item))
      this.modal_data.edit = new_item
      this.show_edit = true
    },
    modal_delete(item) {
      this.modal_data.delete = item
      this.show_delete = true
    },
    modal_clear() {
      this.modal_data.edit = {}
      this.modal_data.delete = {}
      this.show_edit = false
      this.show_delete = false
      Array.from(document.getElementsByClassName('modal')).forEach(el => el.style.display = "none")
      Array.from(document.getElementsByClassName('modal-backdrop')).forEach(el => el.style.display = "none")
    },
    submit_edit(bvModalEvt) {
      bvModalEvt.preventDefault()
      update_items(this.is_admin() ? 'comment' : 'comment_self', [this.modal_data.edit], this.callback)
      this.$nextTick(() => {
        this.show_edit = false
      })
    },
    submit_delete(bvModalEvt) {
      bvModalEvt.preventDefault()
      delete_items(this.is_admin() ? 'comment' : 'comment_self', [this.modal_data.delete], this.callback)
      this.$nextTick(() => {
        this.show_delete = false
      })
    },
    toggle_auto() {
      if(this.auto_interval) {
        clearInterval(this.auto_interval);
      }
      this.auto_mode = !this.auto_mode
      if (this.auto_mode) {
        this.auto_interval = setInterval(this.refresh, 2000);
      }
    },
  },
  watch: {
    current_page: function() {
      this.refresh()
    },
    per_page: function() {
      this.refresh()
    },
  },
}
</script>
