<template>
<span>
  <div v-if="data != ''">
    <b-row v-for="row in data.slice().reverse()" :key="row['uid']">
      <b-col>
        <b-card no-body class='mb-2'>
          <div class='position-absolute' style='top:0!important; right:0!important'>
      	    <b-button v-if="can_edit(row)" size="sm" class='py-0_5 px-1' style='border-radius: 0 0 0 .25rem' variant="primary" @click="modal_edit(row)" v-b-tooltip.hover title="Edit"><i class="la la-pencil-alt la-lg"/></b-button>
      	    <b-button v-if="can_delete(row)" size="sm" class='py-0_5 px-1' style='border-radius: 0 .25rem 0 0' variant="danger" @click="modal_delete(row)" v-b-tooltip.hover title="Delete"><i class="la la-trash la-lg"/></b-button>
          </div>
          <b-card-body class="d-flex p-2 align-items-center">
            <div :class="'bg-' + get_alert_color(row['type']) + ' mr-3 text-white rounded p-2'" v-b-tooltip.hover :title="get_alert_tooltip(row['type'])">
              <i :class="'la ' + get_alert_icon(row['type']) + ' la-2x'"/>
            </div>
            <div>
              <div>
                <span class="font-weight-bold" style="font-size: 1.0rem">{{ row['name'] }}</span>
                <span class="font-italic muted"> @<DateTime :date="row['date']" /></span>
                <span class="text-muted" style="font-size: 0.75rem" v-if="row['edited']"> (edited)</span>
              </div>
              <div class="text-muted">
                {{ row['message'] }}
              </div>
              <div class="text-muted" v-if="row['modifications'] && row['modifications'].length > 0">
                <b-badge variant="warning">modifications</b-badge> <Modification style="font-size: 0.70rem;" :data="row['modifications']"/>
              </div>
            </div>
          </b-card-body>
        </b-card>
      </b-col>
    </b-row>
    <b-form-textarea
      id="textarea"
      v-model="input_text"
      placeholder="Add a comment"
      rows="1"
      max-rows="8"
      class='mb-2'
    ></b-form-textarea>
    <div>
      <b-button size="sm" variant='primary' @click="add_comment(input_text, 'comment')" v-b-tooltip.hover title="Add a comment">Comment</b-button>
      <b-button-group class='float-right ml-2'>
        <b-button size="sm" :variant="auto_mode ? 'success':''" v-b-tooltip.hover :title="auto_mode ? 'Auto Mode ON':'Auto Mode OFF'" @click="toggle_auto()" :pressed.sync="auto_mode"><i v-if="auto_mode" class="la la-eye la-lg"/><i v-else="auto_mode" class="la la-eye-slash la-lg"/></b-button>
        <b-button size="sm" @click="refresh()" v-b-tooltip.hover title="Refresh"><i class="la la-refresh la-lg"/></b-button>
      </b-button-group>
      <div class="d-flex float-right align-items-center ">
        <div class="mr-3">
          <b-pagination
            v-model="current_page"
            :total-rows="nb_rows"
            :per-page="per_page"
            class="m-0"
            size="sm"
          />
        </div>
        <div>
          <b-form-group
            label="Per page"
            label-align="right"
            label-cols="auto"
            label-size="sm"
            label-for="perPageSelect"
            class="m-0"
          >
            <b-form-select
              v-model="per_page"
              id="perPageSelect"
              size="sm"
              :options="page_options"
            />
          </b-form-group>
        </div>
      </div>
    </div>
  </div>
  <b-modal
    id="timeline_edit"
    ref="edit"
    @ok="submit_edit"
    @hidden="modal_clear"
    header-bg-variant="primary"
    header-text-variant="white"
    size ="xl"
    centered
  >
    <template v-slot:modal-title>Edit</template>
    <b-form-group label="Comment:">
      <b-form-input v-model="modal_data.edit.message" />
    </b-form-group>
  </b-modal>
  
  <b-modal
    id="timeline_delete"
    ref="delete"
    @ok="submit_delete"
    @hidden="modal_clear"
    header-bg-variant="danger"
    header-text-variant="white"
    okVariant="danger"
    size="xl"
    centered
  >
    <template v-slot:modal-title>Deleting this item</template>
    <p>{{ modal_data.delete }}</p>
  
  </b-modal>
</span>

</template>

<script>
import DateTime from '@/components/DateTime.vue'
import Modification from '@/components/Modification.vue'
import moment from 'moment'
import { add_items, update_items, delete_items } from '@/utils/api'
import { get_data, get_alert_icon, get_alert_color, get_alert_tooltip } from '@/utils/api'
import { API } from '@/api'

export default {
  name: 'Timeline',
  components: {
    DateTime,
    Modification,
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
      per_page: 5,
      page_options: [5, 10, 20],
      nb_rows: 0,
      current_page: 1,
      orderby: 'date',
      isascending: false,
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
  methods: {
    refresh () {
      console.log(`GET /comment/['=','record_uid','${this.record_data.uid}']`)
      var query = ['=', 'record_uid', this.record_data.uid]
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.orderby !== undefined) { options["orderby"] = this.orderby }
      this.get_data('comment', query, options, this.update_data)
      if (this.auto_mode && this.auto_interval && this.$options.parent._isDestroyed) {
        clearInterval(this.auto_interval)
      }
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
      this.$bvModal.show('timeline_edit')
    },
    modal_delete(item) {
      this.modal_data.delete = item
      this.$bvModal.show('timeline_delete')
    },
    modal_clear() {
      this.modal_data.edit = {}
      this.modal_data.delete = {}
    },
    submit_edit(bvModalEvt) {
      bvModalEvt.preventDefault()
      update_items(this.is_admin() ? 'comment' : 'comment_self', [this.modal_data.edit], this.callback)
      this.$nextTick(() => {
        this.$bvModal.hide('timeline_edit')
      })
    },
    submit_delete(bvModalEvt) {
      bvModalEvt.preventDefault()
      delete_items(this.is_admin() ? 'comment' : 'comment_self', [this.modal_data.delete], this.callback)
      this.$nextTick(() => {
        this.$bvModal.hide('timeline_delete')
      })
    },
    toggle_auto() {
      if(this.auto_interval) {
        clearInterval(this.auto_interval);
      }
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
