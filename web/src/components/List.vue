<template>
  <div>
  <CCard no-body ref="main">
    <CCardHeader class="p-2" v-if="show_tabs">
      <CNav variant="pills" role="tablist" card v-model="tab_index" class='m-0'>
        <CNavItem
          v-for="(tab, i) in tabs"
          v-bind:key="tab.title"
          v-on:click="changeTab(tab)"
        >
          <CNavLink href="javascript:void(0);" :active="tab_index == i">{{ tab.title }}</CNavLink>
        </CNavItem>

        <CNavItem class="ms-auto">
          <CButtonToolbar key-nav>

            <CButtonGroup role="group" class="mx-2" v-if="Array.isArray(selected) && selected.length">
              <!-- Slot for placing buttons that appear only when a selection is made -->
              <slot name="selected_buttons"></slot>
              <CButton
                color="danger"
                v-if="delete_m"
                @click="modal_delete(selected)"
              >Delete selection</CButton>
            </CButtonGroup>

            <CButtonGroup role="group">
              <!-- Slots for placing additional buttons in the header of the table -->
              <slot name="head_buttons"></slot>
              <CButton v-if="add_m" color="success" @click="modal_add()">New</CButton>
              <CButton @click="refreshTable(true)" color="secondary"><i class="la la-refresh la-lg"></i></CButton>
            </CButtonGroup>

          </CButtonToolbar>
        </CNavItem>
      </CNav>
    </CCardHeader>
    <CForm @submit.prevent="">
      <Search @search="search" v-model="search_value" @clear="search_clear" ref='search' class="pt-2 px-2 pb-0">
        <template #search_buttons v-if="!show_tabs">
          <!-- Slots for placing additional buttons in the header of the table -->
          <template v-if="Array.isArray(selected) && selected.length">
            <slot name="selected_buttons"></slot>
            <CButton
              color="danger"
              v-if="delete_m"
              @click="modal_delete(selected)"
            >Delete selection</CButton>
          </template>
          <slot name="head_buttons"></slot>
          <CButton v-if="add_m" color="success" @click="modal_add()">New</CButton>
          <CButton @click="refreshTable(true)" color="secondary" style="border-bottom-right-radius: 0"><i class="la la-refresh la-lg"></i></CButton>
        </template>
      </Search>
    </CForm>
    <CCardBody class="px-2 pb-2 pt-0">
      <CTabContent>
      <SDataTable
        ref="table"
        @cell-clicked="select"
        @celltitle-clicked="selectall_toggle"
        @update:sorter-value="sortingChanged"
        @row-contextmenu="contextMenu"
        v-contextmenu:contextmenu
        :fields="fields"
        :items="items"
        :sorter='{external: true}'
        :sorterValue="{column: orderby, asc: isascending}"
        :loading="is_busy"
        striped
        small
        border
      >
        <template v-for="(_, slot) of $slots" v-slot:[slot]="scope">
          <!-- cell() slots for the b-table -->
          <slot :name="slot" v-bind="scope" />
        </template>

        <template v-slot:timestamp="row">
          <DateTime :date="dig(row.item, 'timestamp')" show_secs />
        </template>
        <template v-slot:message="row">
          {{ truncate_message(dig(row.item, 'message')) }}
        </template>
        <template v-slot:condition="row">
          <Condition :data="dig(row.item, 'condition')" />
        </template>
        <template v-slot:filter="row">
          <Condition :data="dig(row.item, 'filter')" />
        </template>
        <template v-slot:modifications="row">
          <Modification :data="dig(row.item, 'modifications')" />
        </template>
        <template v-slot:fields="row">
          <Field :data="dig(row.item, 'fields')" />
        </template>
        <template v-slot:watch="row">
          <Field :data="dig(row.item, 'watch')" />
        </template>
        <template v-slot:severity="row">
          <Field :data="[dig(row.item, 'severity')]" colorize/>
        </template>
        <template v-slot:ttl="row">
          {{ dig(row.item, 'ttl') >= 0 ? countdown(dig(row.item, 'ttl') - timestamp + dig(row.item, 'date_epoch')) : '-' }}
        </template>
        <template v-slot:permissions="row">
          <Field :data="dig(row.item, 'permissions')" colorize/>
        </template>
        <template v-slot:groups="row">
          <Field :data="dig(row.item, 'groups')" />
        </template>
        <template v-slot:method="row">
          <Field :data="[dig(row.item, 'method')]" colorize/>
        </template>
        <template v-slot:throttle="row">
          {{ pp_countdown(dig(row.item, 'throttle')) }}
        </template>
        <template v-slot:delay="row">
          {{ pp_countdown(dig(row.item, 'delay')) }}
        </template>
        <template v-slot:roles="row">
          <Field :data="(dig(row.item, 'roles') || []).concat(dig(row.item, 'static_roles') || [])" colorize/>
        </template>
        <template v-slot:time_constraints="row">
          <TimeConstraint :date="dig(row.item, 'time_constraints')" />
        </template>
        <template v-slot:state="row">
          <Field :data="[(dig(row.item, 'state') || '-')]" colorize/>
        </template>
        <template v-slot:duplicates="row">
          {{ dig(row.item, 'duplicates') || '1' }}
        </template>
        <template v-slot:discard="row">
          <CBadge v-if="dig(row.item, 'discard')" color="quaternary">yes</CBadge>
          <CBadge v-else color="success">no</CBadge>
        </template>
        <template v-slot:actions="row">
          <Field :data="dig(row.item, 'actions')" />
        </template>
        <template v-slot:enabled="row">
          <Field :data="[(dig(row.item, 'enabled') == undefined || dig(row.item, 'enabled') == true) ? 'enabled' : 'disabled']" colorize/>
        </template>
        <template v-slot:pprint="row">
          <table class="table-borderless"><tr style="background-color: transparent !important"><td class="p-0 pe-1"><i :class="'la la-'+dig(row.item, 'icon')+' la-lg'"></i></td><td class="p-0"><b>{{ dig(row.item, 'widget', 'selected') || '' + dig(row.item, 'action', 'selected') || '' }}</b> @ {{ dig(row.item, 'pprint') }}</td></tr></table>
        </template>
        <template v-slot:color="row">
          <ColorBadge :data="dig(row.item, 'color') || '#ffffff'" />
        </template>
        <template v-slot:login="row">
          <DateTime :date="dig(row.item, 'last_login') || '0'"/>
        </template>
        <template v-slot:select="row">
          <input type="checkbox" class="pointer mx-1" :checked="dig(row.item, '_selected') == true">
        </template>
        <template v-slot:select-header="row">
          <input type="checkbox" class="pointer mx-1" :checked="this.items.length == this.selected.length">
        </template>

        <template v-slot:button="row">
          <CButtonGroup role="group">
            <!-- Action buttons -->
            <CButton color="secondary" size="sm" @click="toggleDetails(row.item, $event)">
              <i v-if="Boolean(row.item._showDetails)" class="la la-angle-up la-lg"></i>
              <i v-else class="la la-angle-down la-lg"></i>
            </CButton>
            <slot name="custom_buttons" v-bind="row" />
            <CButton v-if="edit_m" size="sm" @click="modal_edit(row.item)" color="primary" v-c-tooltip="{content: 'Edit'}"><i class="la la-pencil-alt la-lg"></i></CButton>
            <CButton v-if="delete_m" size="sm" @click="modal_delete([row.item])" color="danger" v-c-tooltip="{content: 'Delete'}"><i class="la la-trash la-lg"></i></CButton>
          </CButtonGroup>
        </template>
        <template v-slot:details="row">
          <CCollapse :visible="Boolean(row.item._showDetails)">
            <CCard v-if="Boolean(row.item._showDetails)">
              <CRow class="m-0">
                <CCol class="p-2">
                  <slot name="info" v-bind="row" />
                  <Info :myobject="row.item" :excluded_fields="info_excluded_fields" />
                </CCol>
                <slot name="details_side" v-bind="row" />
              </CRow>
              <CButton size="sm" @click="toggleDetails(row.item, $event)"><i class="la la-angle-up la-lg"></i></CButton>
            </CCard>
          </CCollapse>
        </template>
      </SDataTable>
      <div class="d-flex align-items-center">
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
                :value="per_page"
                id="perPageSelect"
                size="sm"
              >
                <option v-for="opts in page_options" :value="opts">{{ opts }}</option>
              </CFormSelect>
            </CCol>
          </CRow>
        </div>
      </div>
      </CTabContent>
    </CCardBody>
  </CCard>

  <CModal
    ref="edit"
    :visible="show_edit"
    @close="modal_clear"
    size ="xl"
    alignment="center"
    backdrop="static"
  >
    <CModalHeader class="bg-primary">
      <CModalTitle class="text-white">{{ modal_title_edit }}</CModalTitle>
    </CModalHeader>
    <CModalBody>
      <CForm @submit.stop.prevent="checkForm" novalidate ref="edit_form">
        <Form v-model="modal_data.edit" :metadata="form" />
      </CForm>
    </CModalBody>
    <CModalFooter>
      <CButton @click="modal_clear" color="secondary">Cancel</CButton>
      <CButton @click="submit_edit" color="primary">OK</CButton>
    </CModalFooter>
  </CModal>

  <CModal
    ref="add"
    :visible="show_add"
    @close="modal_clear"
    size="xl"
    alignment="center"
    backdrop="static"
  >
    <CModalHeader class="bg-success">
      <CModalTitle class="text-white">{{ modal_title_add }}</CModalTitle>
    </CModalHeader>
    <CModalBody>
      <CForm @submit.stop.prevent="checkForm" novalidate ref="add_form">
        <Form v-model="modal_data.add" :metadata="form" />
      </CForm>
    </CModalBody>
    <CModalFooter>
      <CButton @click="modal_clear" color="secondary">Cancel</CButton>
      <CButton @click="submit_add" color="success">OK</CButton>
    </CModalFooter>
  </CModal>

  <CModal
    ref="delete"
    :visible="show_delete"
    @close="modal_clear"
    size="xl"
    alignment="center"
    backdrop="static"
  >
    <CModalHeader class="bg-danger">
      <CModalTitle class="text-white" v-if="modal_data.delete.length > 1">Delete {{ modal_data.delete.length }} items</CModalTitle>
      <CModalTitle class="text-white" v-else>{{ modal_title_delete }}</CModalTitle>
    </CModalHeader>
    <CModalBody>
      <p v-if="modal_data.delete.length > 1">This operation cannot be undone. Are you sure?</p>
      <p v-else>{{ modal_data.delete[0] }}</p>
    </CModalBody>
    <CModalFooter>
      <CButton @click="modal_clear" color="secondary">Cancel</CButton>
      <CButton @click="submit_delete" color="danger">OK</CButton>
    </CModalFooter>
  </CModal>

  <v-contextmenu ref="contextmenu" @show="store_selection">
    <v-contextmenu-item @click="copy_browser">
      <i class="la la-copy la-lg"></i> Copy
    </v-contextmenu-item>
    <v-contextmenu-item @click="select_all">
      <i class="la la-check-square la-lg"></i> Select All
    </v-contextmenu-item>
    <v-contextmenu-item @click="context_search">
      <i class="la la-search la-lg"></i> Search
    </v-contextmenu-item>
    <v-contextmenu-submenu title="To Clipboard">
      <template v-slot:title><i class="la la-clipboard la-lg"></i> To Clipboard</template>
      <v-contextmenu-item @click="copy_clipboard" method="yaml">
        As YAML
      </v-contextmenu-item>
      <v-contextmenu-item @click="copy_clipboard" method="yaml" full="true">
        As YAML (Full)
      </v-contextmenu-item>
      <v-contextmenu-divider />
      <v-contextmenu-item @click="copy_clipboard" method="json">
        As JSON
      </v-contextmenu-item>
      <v-contextmenu-item @click="copy_clipboard" method="json" full="true">
        As JSON (Full)
      </v-contextmenu-item>
      <v-contextmenu-divider />
      <v-contextmenu-item v-for="field in fields.filter(field => field.key != 'button' && field.key != 'select')" :key="field.key" @click="copy_clipboard" method="simple" :field="field.key">
        {{ capitalizeFirstLetter(field.key) }}
      </v-contextmenu-item>
    </v-contextmenu-submenu>
  </v-contextmenu>

  </div>
</template>

<script>
import dig from 'object-dig'
import moment from 'moment'
import { API } from '@/api'
import { get_data, pp_countdown, countdown, preprocess_data, delete_items, truncate_message, to_clipboard, capitalizeFirstLetter } from '@/utils/api'
import { join_queries } from '@/utils/query'
import Form from '@/components/Form.vue'
import Search from '@/components/Search.vue'
import Condition from '@/components/Condition.vue'
import Modification from '@/components/Modification.vue'
import Field from '@/components/Field.vue'
import DateTime from '@/components/DateTime.vue'
import TimeConstraint from '@/components/TimeConstraint.vue'
import Info from '@/components/Info.vue'
import ColorBadge from '@/components/ColorBadge.vue'
import SDataTable from '@/components/SDataTable.vue'
import SPagination from '@/components/SPagination.vue'
const yaml = require('js-yaml')

// Create a table representing an API endpoint.
export default {
  name: 'List',
  components: {
    Condition,
    Modification,
    Field,
    DateTime,
    TimeConstraint,
    Search,
    Form,
    Info,
    ColorBadge,
    SDataTable,
    SPagination,
  },
  props: {
    // The tabs name and their associated search
    tabs_prop: {
      type: Array,
      default: () => { return [] },
    },
    // The API path to query
    endpoint_prop: {
      type: String,
      required: true,
    },
    // An array containing the fields to pass to the `CTable`
    fields_prop: {
      type: Array,
      default: () => { return [] },
    },
    // An array containing the hidden fields used for searching
    hidden_fields_prop: {
      type: Array,
      default: () => { return [] },
    },
    // An object describing the input form for editing/adding
    form_prop: {
      type: Object,
      default: () => { return {} },
    },
    // Allow the `Add` button
    add_mode: {type: Boolean, default: false},
    // Allow the `Edit` button in actions
    edit_mode: {type: Boolean, default: false},
    // Allow the `Delete` button in actions
    delete_mode: {type: Boolean, default: false},
    // The default key to order by
    order_by: {type: String, default: undefined},
    // Ascending (true) or Descending (false)
    is_ascending: {type: Boolean, default: false},
    // List of fields to exclude from Info, as they will be displayed
    // in a custom view.
    info_excluded_fields: {type: Array, default: () => []},
    modal_title_add: {type: String, default: 'New'},
    modal_title_edit: {type: String, default: 'Edit'},
    modal_title_delete: {type: String, default: 'Delete this item'},
    show_tabs: {type: Boolean, default: false},
  },
  mounted () {
    this.settings = JSON.parse(localStorage.getItem(this.endpoint+'_json') || '{}')
    get_data('settings/?c='+encodeURIComponent(`web/${this.endpoint}`)+'&checksum='+(this.settings.checksum || ""), null, {}, this.load_table)
  },
  unmounted () {
    this.emitter.all.clear()
  },
  data () {
    return {
      busy_interval: null,
      is_busy: false,
      to_clipboard:to_clipboard,
      dig: dig,
      pp_countdown: pp_countdown,
      countdown: countdown,
      preprocess_data: preprocess_data,
      join_queries: join_queries,
      truncate_message: truncate_message,
      capitalizeFirstLetter: capitalizeFirstLetter,
      timestamp: {},
      delete_items: delete_items,
      filter: [],
      env_name: '',
      env_filter: [],
      tab_index: 0,
      search_value: '',
      per_page: '20',
      page_options: ['20', '50', '100'],
      nb_rows: 0,
      current_page: 1,
      items: [],
      item_copy: {},
      adding_data: {},
      selected_text: '',
      selected_data: {},
      selected: [],
      settings: {},
      loaded: false,
      endpoint: this.endpoint_prop,
      tabs: this.tabs_prop,
      form: this.form_prop,
      default_fields: this.fields_prop,
      fields: this.fields_prop,
      default_hidden_fields: this.hidden_fields_prop,
      hidden_fields: this.hidden_fields_prop,
      default_orderby: this.order_by,
      orderby: this.order_by,
      default_isascending: this.is_ascending,
      isascending: this.is_ascending,
      add_m: this.add_mode,
      edit_m: this.edit_mode,
      delete_m: this.delete_mode,
      show_edit: false,
      show_add: false,
      show_delete: false,
      modal_data: {
        add: {},
        edit: {},
        delete: [],
      },
    }
  },
  computed: {
  },
  methods: {
    load_table(response) {
      if (response.data) {
        if (response.data.count > 0) {
          this.settings = response.data
          localStorage.setItem(this.endpoint+'_json', JSON.stringify(response.data))
        }
        var data = this.settings.data[0]
        this.tabs = dig(data, 'tabs') || this.tabs
        this.form = dig(data, 'form')
        this.endpoint = dig(data, 'endpoint') || this.endpoint
        this.orderby = dig(data, 'orderby') || this.orderby
        this.default_orderby = this.orderby
        this.fields = dig(data, 'fields')
        this.fields.splice(0, 0, { key: 'select', label: '', tdClass: ['align-middle'], clickable: true, clickable_title: true })
        this.default_fields = this.fields
        this.hidden_fields = dig(data, 'hidden_fields') || []
        this.default_hidden_fields = this.hidden_fields
        this.isascending = dig(data, 'isascending') || false
        this.default_isascending = this.isascending
        this.filter = this.tabs[0].filter
        this.reload()
        this.get_now()
        setInterval(this.get_now, 1000);
        this.emitter.on('environment_change_tab', tab => {
          this.env_name = tab.name
          this.env_filter = tab.filter
          this.add_history()
        })
      }
    },
    reload() {
      var tab = this.tabs[0]
      if (this.$route.query.tab !== undefined) {
        var find_tab = this.tabs.find(el => el.title == this.$route.query.tab)
        if (find_tab) {
          tab = find_tab
          this.tab_index = this.tabs.indexOf(this.tab_index)
          this.filter = tab.filter
        }
      }
      if (tab) {
        this.changeTab(tab, false)
      }
      if (this.$route.query.env_filter !== undefined) {
        this.env_filter = JSON.parse(decodeURIComponent(this.$route.query.env_filter))
      } else {
        this.env_filter = []
      }
      if (this.$route.query.env_name !== undefined) {
        this.env_name = decodeURIComponent(this.$route.query.env_name)
      } else {
        this.env_name = ''
      }
      if (this.$route.query.perpage !== undefined) {
        this.per_page = this.$route.query.perpage
      }
      if (this.$route.query.pagenb !== undefined) {
        this.current_page = parseInt(this.$route.query.pagenb)
      }
      if (this.$route.query.asc !== undefined) {
        this.isascending = JSON.parse(this.$route.query.asc)
      }
      if (this.$route.query.orderby !== undefined) {
        this.orderby = this.$route.query.orderby
      }
      if (this.$route.query.s !== undefined) {
        var decoded_query = decodeURIComponent(this.$route.query.s)
        if (this.$refs.search) {
          this.search_value = decoded_query
          this.$refs.search.datavalue = decoded_query
        }
        this.refreshTable()
      } else {
        if (this.$refs.search) {
          this.search_value = ''
          this.$refs.search.datavalue = ''
        }
        this.refreshTable()
      }
    },
    get_now() {
      this.timestamp = moment().unix()
    },
    refreshTable(feedback = false) {
      this.clearSelected()
      this.set_busy(true)
      if (this.tabs[this.tab_index]) {
        this.filter = this.tabs[this.tab_index].filter
      }
      var query = this.filter
      if (this.env_filter.length > 0) {
        query = join_queries([this.filter, this.env_filter])
      }
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.search_value) {
        options["ql"] = this.search_value
      }
      if (this.orderby !== undefined) {
        var form_field = this.fields.concat(this.hidden_fields).find((field, ) => field.key == this.orderby)
        if (form_field && form_field.orderby) {
          options["orderby"] = form_field.orderby
        } else {
          options["orderby"] = this.orderby
        }
      }
      get_data(this.endpoint, query, options, feedback == true ? this.feedback_then_update : this.update_table, null)
    },
    feedback_then_update(response) {
      this.$root.show_alert()
      this.update_table(response)
    },
    checkForm(node) {
      return (node.$el.getElementsByClassName('form-control is-invalid').length + node.$el.getElementsByClassName('has-error').length) == 0
    },
    submit_edit(bvModalEvt, endpoint = this.endpoint) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.edit_form)) {
        this.$root.text_alert('Form is invalid', 'danger')
        return
      }
      var data = this.modal_data.edit
      var filtered_object = this.preprocess_data(data)
      this.set_busy(true)
      console.log(`PUT /${endpoint}`)
      API
        .put(`/${endpoint}`, [filtered_object])
        .then(response => {
          this.set_busy(false)
          if (response.data) {
            if (response.data.data.rejected.length > 0) {
              this.$root.text_alert('Cannot Edit', 'danger')
            } else {
              this.refreshTable()
              this.$root.text_alert('Entry updated successfully', 'success')
            }
          } else {
            if(response.response.data.description) {
              this.$root.text_alert(response.response.data.description, 'danger')
            } else {
              this.$root.text_alert('Could not update the entry', 'danger')
            }
          }
        })
        .catch(error => console.log(error))
      this.$nextTick(() => {
        this.modal_clear()
      })
    },
    submit_add(bvModalEvt, endpoint = this.endpoint) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.add_form)) {
        this.$root.text_alert('Form is invalid', 'danger', 'Error')
        return
      }
      var data = this.modal_data.add
      var filtered_object = this.preprocess_data(data)
      this.set_busy(true)
      console.log(`POST /${endpoint}`)
      API
        .post(`/${endpoint}`, [filtered_object])
        .then(response => {
          this.set_busy(false)
          if (response.data) {
            if (response.data.data.rejected.length > 0) {
              this.$root.text_alert('Cannot Add', 'danger')
            } else {
              this.refreshTable()
              this.$root.text_alert('Entry added successfully', 'success')
            }
          } else {
            if(response.response.data.description) {
              this.$root.text_alert(response.response.data.description, 'danger')
            } else {
              this.$root.text_alert('Could not add the entry', 'danger')
            }
          }
        })
        .catch(error => console.log(error))
      this.$nextTick(() => {
        this.modal_clear()
      })
    },
    submit_delete(bvModalEvt, endpoint = this.endpoint) {
      this.set_busy(true)
      delete_items(endpoint, this.modal_data.delete, () => { this.set_busy(false); this.refreshTable()})
      this.$nextTick(() => {
        this.modal_clear()
      })
    },
    update_table(response) {
      this.set_busy(false)
      if (response.data) {
        this.items = []
        this.nb_rows = response.data.count
        var rows = response.data.data || []
        rows.forEach(row => {
          if ( this.items.every(x => x['uid'] != row['uid']) ) {
            this.items.push(row)
          }
        })
      }
      this.loaded = true
    },
    search(query) {
      //console.log(`Search: ${query}`)
      this.add_history()
    },
    search_clear() {
      this.search_value = ''
      this.add_history()
    },
    changeTab(tab, refresh = true) {
      this.tab_index = this.tabs.indexOf(tab)
      this.filter = tab.filter
      if (tab.fields) {
        this.fields = tab.fields
      } else {
        this.fields = this.default_fields
      }
      if (tab.orderby) {
        this.orderby = tab.orderby
      } else {
        this.orderby = this.default_orderby
      }
      if (tab.hidden_fields) {
        this.hidden_fields = tab.hidden_fields
      } else {
        this.hidden_fields = this.default_hidden_fields
      }
      if (tab.isascending) {
        this.isascending = tab.isascending
      } else {
        this.isascending = this.default_isascending
      }
      if (tab.handler) {
        tab.handler(tab)
      }
      if (refresh) {
        this.add_history()
      }
    },
    set_busy(busy) {
      if (this.busy_interval) {
        clearInterval(this.busy_interval)
        this.busy_interval = null
      }
      if (busy) {
        this.busy_interval = setInterval(() => {this.is_busy = true}, 500);
      } else {
        this.is_busy = false
      }
    },
    select (item, colName, index, e) {
      var found = this.selected.indexOf(item)
      if (found >= 0) {
        this.selected.splice(found, 1)
        item._selected = false
      } else {
        this.selected.push(item)
        item._selected = true
      }
    },
    modal_add () {
      this.modal_data.add = {}
      if (this.tabs[this.tab_index]['parent']) {
        this.modal_data.add['parent'] = this.tabs[this.tab_index]['parent']
      }
      this.show_add = true
    },
    modal_edit (item) {
      var new_item = JSON.parse(JSON.stringify(item))
      this.modal_data.edit = new_item
      this.show_edit = true
    },
    modal_delete(items) {
      this.modal_data.delete = items
      this.show_delete = true
    },
    modal_clear() {
      this.modal_data.add = {}
      this.modal_data.edit = {}
      this.modal_data.delete = []
      this.show_add = false
      this.show_edit = false
      this.show_delete = false
      Array.from(document.getElementsByClassName('modal')).forEach(el => el.style.display = "none")
      Array.from(document.getElementsByClassName('modal-backdrop')).forEach(el => el.style.display = "none")
    },
    sortingChanged (ctx) {
      this.orderby = ctx.column
      this.isascending = ctx.asc
      this.add_history()
    },
    clearSelected() {
      this.items.forEach(item => {
        item._selected = false
      })
      this.selected = []
    },
    select_all() {
      this.selected = []
      this.items.forEach(item => {
        item._selected = true
        this.selected.push(item)
      })
    },
    selectall_toggle (name, index) {
      if(this.items.length != this.selected.length) {
        this.select_all()
      } else {
        this.clearSelected()
      }
    },
    add_history() {
      const query = { tab: this.tabs[this.tab_index].title, s: (this.search_value || ''), env_name: this.env_name, env_filter: encodeURIComponent(JSON.stringify(this.env_filter)),
        perpage: this.per_page, pagenb: this.current_page, orderby: this.orderby, asc: this.isascending }
      if (this.$route.query.tab != query.tab || this.$route.query.s != query.s || this.$route.query.env_name != query.env_name
        || this.$route.query.perpage != query.perpage || this.$route.query.pagenb != query.pagenb || this.$route.query.asc != query.asc || this.$route.query.orderby != query.orderby) {
        this.$router.push({ query: query })
      }
    },
    contextMenu(item, index, colname, event) {
      event.preventDefault()
      this.item_copy = item
      this.$refs.contextmenu.hide()
      this.$refs.contextmenu.show({top: event.pageY, left: event.pageX})
    },
    get_fields(row, selected_fields = {}) {
      var return_obj = Object.keys(row).filter(key => key[0] != '_' && key != 'button')
      if (Object.keys(selected_fields).length > 0) {
        var filtered_fields = selected_fields.reduce((obj, key) => {
          obj.push(key.key)
          return obj
        }, [])
        return_obj = return_obj.filter(key => filtered_fields.includes(key))
      }
      return return_obj.reduce((obj, key) => {
        obj.push({name: key, value: row[key]})
        return obj
      }, [])
    },
    add_clipboard(row, parse_fun, selected_fields = {}) {
      if (row) {
        var output = {}
        this.get_fields(row, selected_fields).forEach(field => {
          output[field.name] = field.value
        })
      	this.to_clipboard(parse_fun(output))
      }
    },
    copy_clipboard(event) {
      var method
      var fields = this.fields
      if (event.target.attributes.method.value == 'yaml') {
        method = yaml.dump
      } else if (event.target.attributes.method.value == 'json') {
        method = JSON.stringify
      } else {
        this.to_clipboard(yaml.dump(this.item_copy[event.target.attributes.field.value], { flowLevel: 0 }).slice(0, -1))
        return
      }
      if (event.target.attributes.full) {
        fields = {}
      }
      this.add_clipboard(this.item_copy, method, fields)
    },
    store_selection() {
      this.selectedText = window.getSelection().toString()
    },
    copy_browser(event) {
      this.to_clipboard(this.selectedText)
    },
    context_search(event) {
      if (this.selectedText != '') {
        this.search_value = this.selectedText
        this.$refs.search.datavalue = this.selectedText
        this.search(this.selectedText)
      }
    },
    toggleDetails(row, event) {
      event.stopPropagation()
      row._showDetails = !row._showDetails
    },
  },
  watch: {
    current_page: function() {
      this.add_history()
    },
    per_page: function() {
      this.add_history()
    },
    $route() {
      if (this.loaded && this.$route.path == `/${this.endpoint}`) {
        this.$nextTick(this.reload);
      }
    }
  },
}
</script>

<style>

.fix-nav {
  height: 100%;
}

</style>
