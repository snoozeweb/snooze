<template>
  <div>
  <b-card no-body ref="main">
    <b-card-header header-tag="nav" class="p-2">
      <b-nav card-header pills class='m-0'>
        <b-nav-item
          v-for="(tab, index) in tabs"
          v-bind:key="tab.title"
          :active="index == tab_index"
          v-on:click="changeTab(tab)"
        >
          {{ tab.title }}
        </b-nav-item>

        <b-nav-item class="ml-auto" link-classes="py-0 pr-0">
          <b-button-toolbar key-nav>

            <b-button-group class="mx-2" v-if="Array.isArray(selected) && selected.length">
              <!-- Slot for placing buttons that appear only when a selection is made -->
              <slot name="selected_buttons"></slot>
              <b-button @click="clearSelected">Clear selection</b-button>
              <b-button
                variant="danger"
                v-if="delete_m"
                @click="delete_items(endpoint, selected)"
              >Delete selection</b-button>
            </b-button-group>

            <b-button-group>
              <!-- Slots for placing additional buttons in the header of the table -->
              <b-button @click="select_all">Select All</b-button>
              <slot name="head_buttons"></slot>
              <b-button v-if="add_m" variant="success" @click="modal_add()">New</b-button>
              <b-button @click="refresh(true)"><i class="la la-refresh la-lg"></i></b-button>
            </b-button-group>

          </b-button-toolbar>
        </b-nav-item>
      </b-nav>

    </b-card-header>
    <b-form>
      <Search @search="search($event)" @clear="search_clear()" ref='search' class="pt-2 px-2"/>
    </b-form>
    <b-card-body class="p-2">
      <b-table
        ref="table"
        @row-selected="select"
        @sort-changed="sortingChanged"
        @row-contextmenu="contextMenu"
        :fields="fields"
        :items="items"
        :no-local-sorting="true"
        :sort-by.sync="orderby"
        :sort-desc.sync="isascending"
        :busy="is_busy"
        selectable
        select-mode="range"
        selectedVariant="info"
        striped
        small
        bordered
      >
        <template #table-busy>
          <div class="text-center text-dark my-2">
            <b-spinner class="align-middle"></b-spinner>
            <strong> Loading...</strong>
          </div>
        </template>

        <template v-for="(_, slot) of $scopedSlots" v-slot:[slot]="scope">
          <!-- cell() slots for the b-table -->
          <slot :name="slot" v-bind="scope" />
        </template>

        <template v-slot:cell(timestamp)="row">
          <DateTime :date="dig(row.item, 'timestamp')" show_secs />
        </template>
        <template v-slot:cell(message)="row">
          {{ truncate_message(dig(row.item, 'message')) }}
        </template>
        <template v-slot:cell(condition)="row">
          <Condition :data="dig(row.item, 'condition')" />
        </template>
        <template v-slot:cell(filter)="row">
          <Condition :data="dig(row.item, 'filter')" />
        </template>
        <template v-slot:cell(modifications)="row">
          <Modification :data="dig(row.item, 'modifications')" />
        </template>
        <template v-slot:cell(fields)="row">
          <Field :data="dig(row.item, 'fields')" />
        </template>
        <template v-slot:cell(watch)="row">
          <Field :data="dig(row.item, 'watch')" />
        </template>
        <template v-slot:cell(severity)="row">
          <Field :data="[dig(row.item, 'severity')]" colorize/>
        </template>
        <template v-slot:cell(ttl)="row">
          {{ dig(row.item, 'ttl') >= 0 ? countdown(dig(row.item, 'ttl') - timestamp + dig(row.item, 'date_epoch')) : '-' }}
        </template>
        <template v-slot:cell(permissions)="row">
          <Field :data="dig(row.item, 'permissions')" colorize/>
        </template>
        <template v-slot:cell(groups)="row">
          <Field :data="dig(row.item, 'groups')" />
        </template>
        <template v-slot:cell(method)="row">
          <Field :data="[dig(row.item, 'method')]" colorize/>
        </template>
        <template v-slot:cell(throttle)="row">
          {{ pp_countdown(dig(row.item, 'throttle')) }}
        </template>
        <template v-slot:cell(delay)="row">
          {{ pp_countdown(dig(row.item, 'delay')) }}
        </template>
        <template v-slot:cell(roles)="row">
          <Field :data="(dig(row.item, 'roles') || []).concat(dig(row.item, 'static_roles') || [])" colorize/>
        </template>
        <template v-slot:cell(time_constraints)="row">
          <TimeConstraint :date="dig(row.item, 'time_constraints')" />
        </template>
        <template v-slot:cell(state)="row">
          <Field :data="[(dig(row.item, 'state') || '-')]" colorize/>
        </template>
        <template v-slot:cell(discard)="row">
          <b-badge v-if="dig(row.item, 'discard')" variant="quaternary">yes</b-badge>
          <b-badge v-else variant="success">no</b-badge>
        </template>
        <template v-slot:cell(actions)="row">
          <Field :data="dig(row.item, 'actions')" />
        </template>
        <template v-slot:cell(enabled)="row">
          <Field :data="[(dig(row.item, 'enabled') == undefined || dig(row.item, 'enabled') == true) ? 'enabled' : 'disabled']" colorize/>
        </template>
        <template v-slot:cell(pprint)="row">
          <table class="table-borderless"><tr style="background-color: transparent !important"><td class="p-0 pr-1"><i :class="'la la-'+dig(row.item, 'icon')+' la-lg'"></i></td><td class="p-0"><b>{{ dig(row.item, 'widget', 'selected') || '' + dig(row.item, 'action', 'selected') || '' }}</b> @ {{ dig(row.item, 'pprint') }}</td></tr></table>
        </template>
        <template v-slot:cell(color)="row">
          <ColorBadge :data="dig(row.item, 'color') || '#ffffff'" />
        </template>
        <template v-slot:cell(login)="row">
          <DateTime :date="dig(row.item, 'last_login') || '0'"/>
        </template>

        <template v-slot:cell(button)="row">
          <b-button-group>
            <!-- Action buttons -->
            <b-button size="sm" @click="row.toggleDetails">
              <i v-if="row.detailsShowing" class="la la-angle-up la-lg"></i>
              <i v-else class="la la-angle-down la-lg"></i>
            </b-button>
            <slot name="button" v-bind="row" />
            <b-button v-if="edit_m" size="sm" @click="modal_edit(row.item)" variant="primary" v-b-tooltip.hover title="Edit"><i class="la la-pencil-alt la-lg"></i></b-button>
            <b-button v-if="delete_m" size="sm" @click="modal_delete(row.item)" variant="danger" v-b-tooltip.hover title="Delete"><i class="la la-trash la-lg"></i></b-button>
          </b-button-group>
        </template>
        <template v-slot:row-details="row">
          <b-card body-class="p-2" bg-variant="light">
          <b-row>
            <b-col>
              <slot name="info" v-bind="row" />
              <Info :myobject="row.item" :excluded_fields="info_excluded_fields" />
            </b-col>
            <slot name="details_side" v-bind="row"></slot>
          </b-row>
            <b-button size="sm" @click="row.toggleDetails"><i class="la la-angle-up la-lg"></i></b-button>
          </b-card>
        </template>
      </b-table>
      <div class="d-flex align-items-center">
        <div class="mr-3">
          <b-pagination
            v-model="current_page"
            :total-rows="nb_rows"
            :per-page="per_page"
            class="m-0"
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
    </b-card-body>
  </b-card>

  <b-modal
    id="edit"
    ref="edit"
    @ok="submit_edit"
    @hidden="modal_clear"
    header-bg-variant="primary"
    header-text-variant="white"
    size ="xl"
    centered
  >
    <template v-slot:modal-title>{{ modal_title_edit }}</template>
    <b-form @submit.stop.prevent="checkForm" novalidate ref="edit_form">
      <Form v-model="modal_data.edit" :metadata="form" />
    </b-form>
  </b-modal>

  <b-modal
    id="add"
    ref="add"
    @ok="submit_add"
    @hidden="modal_clear"
    header-bg-variant="success"
    header-text-variant="white"
    okVariant="success"
    size="xl"
    centered
  >
    <template v-slot:modal-title>{{ modal_title_add }}</template>
    <b-form @submit.stop.prevent="checkForm" novalidate ref="add_form">
      <Form v-model="modal_data.add" :metadata="form" />
    </b-form>
  </b-modal>

  <b-modal
    id="delete"
    ref="delete"
    @ok="submit_delete"
    @hidden="modal_clear"
    header-bg-variant="danger"
    header-text-variant="white"
    okVariant="danger"
    size="xl"
    centered
  >
    <template v-slot:modal-title>{{ modal_title_delete }}</template>
    <p>{{ modal_data.delete }}</p>
  </b-modal>

  <b-alert
    :show="alert_countdown"
    dismissible
    fade
    class="position-fixed fixed-top m-0 rounded-0 text-center"
    style="z-index: 2000;"
    variant="success"
    @dismiss-count-down="a => this.alert_countdown = a"
  >
    Updated
  </b-alert>

  <v-contextmenu ref="contextmenu">
    <v-contextmenu-submenu>
      <template v-slot:title><i class="la la-copy la-lg"/> Copy</template>
      <v-contextmenu-item @click="copy_clipboard" method="yaml">
        As YAML
      </v-contextmenu-item>
      <v-contextmenu-item @click="copy_clipboard" method="yaml" full="true">
        As YAML (Full)
      </v-contextmenu-item>
      <v-contextmenu-item divider></v-contextmenu-item>
      <v-contextmenu-item @click="copy_clipboard" method="json">
        As JSON
      </v-contextmenu-item>
      <v-contextmenu-item @click="copy_clipboard" method="json" full="true">
        As JSON (Full)
      </v-contextmenu-item>
      <v-contextmenu-item divider></v-contextmenu-item>
      <v-contextmenu-item v-for="field in fields.filter(field => field.key != 'button')" :key="field.key" @click="copy_clipboard" method="simple" :value="field.key">
        {{ field.key.charAt(0).toUpperCase() + field.key.slice(1) }}
      </v-contextmenu-item>
    </v-contextmenu-submenu>
  </v-contextmenu>

  </div>
</template>

<script>
import dig from 'object-dig'
import moment from 'moment'
import { API } from '@/api'
import { get_data, pp_countdown, countdown, preprocess_data, delete_items, truncate_message, to_clipboard } from '@/utils/api'
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
  },
  props: {
    // The tabs name and their associated search
    tabs: {
      type: Array,
      required: true
    },
    // The API path to query
    endpoint: {
      type: String,
      required: true,
    },
    // An array containing the fields to pass to the `b-table`
    fields: {
      type: Array,
      required: true,
    },
    // An array containing the hidden fields used for searching
    hidden_fields: {
      type: Array,
      required: false,
      default: () => { return [] },
    },
    // An object describing the input form for editing/adding
    form: {
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
  },
  mounted () {
    this.reload()
    this.get_now()
    setInterval(this.get_now, 1000);
    this.$root.$on('environment_change_tab', (tab) => {
      this.env_name = tab.name
      this.env_filter = tab.filter
      this.refreshTable()
      this.add_history()
    })
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
      get_data: get_data,
      join_queries: join_queries,
      truncate_message: truncate_message,
      alert_countdown: 0,
      timestamp: {},
      delete_items: delete_items,
      filter: this.tabs[0].filter,
      env_name: '',
      env_filter: [],
      tab_index: 0,
      search_data: '',
      per_page: 20,
      page_options: [20, 50, 100],
      nb_rows: 0,
      current_page: 1,
      items: [],
      item_copy: {},
      adding_data: {},
      selected_data: {},
      selected: [],
      orderby: this.order_by,
      isascending: this.is_ascending,
      add_m: this.add_mode,
      edit_m: this.edit_mode,
      delete_m: this.delete_mode,
      modal_data: {
        add: {},
        edit: {},
        delete: {},
      },
    }
  },
  computed: {
  },
  methods: {
    reload() {
      var tab = this.tabs[0]
      if (this.$route.query.tab !== undefined) {
        var find_tab = this.tabs.find(el => el.title == this.$route.query.tab)
        if (tab) {
          tab = find_tab
          this.tab_index = this.tabs.indexOf(this.tab_index)
          this.filter = tab.filter
        }
      }
      this.changeTab(tab, false)
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
      if (this.$route.query.s !== undefined) {
        var decoded_query = decodeURIComponent(this.$route.query.s)
        this.$refs.search.datavalue = decoded_query
        this.search_data = decoded_query
        this.refreshTable()
      } else {
        this.$refs.search.datavalue = ''
        this.refreshTable()
      }
    },
    get_now() {
      this.timestamp = moment().unix()
    },
    refresh(feedback = false) {
      this.filter = this.tabs[this.tab_index].filter
      var query = this.filter
      if (this.env_filter.length > 0) {
        query = join_queries([this.filter, this.env_filter])
      }
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.search_data) {
        options["ql"] = this.search_data
      }
      if (this.orderby !== undefined) {
        var form_field = this.fields.concat(this.hidden_fields).find((field, ) => field.key == this.orderby)
        if (form_field && form_field.orderby) {
          options["orderby"] = form_field.orderby
        } else {
          options["orderby"] = this.orderby
        }
      }
      this.get_data(this.endpoint, query, options, feedback ? this.feedback_then_update : this.update_table, null)
    },
    feedback_then_update(response) {
      this.alert_countdown = 1
      this.update_table(response)
    },
    checkForm(node) {
      return (node.getElementsByClassName('form-control is-invalid').length + node.getElementsByClassName('has-error').length) == 0
    },
    submit_edit(bvModalEvt, endpoint = this.endpoint) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.edit_form)) {
        this.makeToast('Form is invalid', 'danger', 'Error')
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
              this.makeToast('Cannot Edit', 'danger', 'An error occurred')
            } else {
              this.refreshTable()
              this.makeToast('Entry updated successfully', 'success')
            }
          } else {
            if(response.response.data.description) {
              this.makeToast(response.response.data.description, 'danger', 'An error occurred')
            } else {
              this.makeToast('Could not update the entry', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
      this.$nextTick(() => {
        this.$bvModal.hide('edit')
      })
    },
    submit_add(bvModalEvt, endpoint = this.endpoint) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.add_form)) {
        this.makeToast('Form is invalid', 'danger', 'Error')
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
              this.makeToast('Cannot Add', 'danger', 'An error occurred')
            } else {
              this.refreshTable()
              this.makeToast('Entry added successfully', 'success')
            }
          } else {
            if(response.response.data.description) {
              this.makeToast(response.response.data.description, 'danger', 'An error occurred')
            } else {
              this.makeToast('Could not add the entry', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
      this.$nextTick(() => {
        this.$bvModal.hide('add')
      })
    },
    submit_delete(bvModalEvt, endpoint = this.endpoint) {
      var uid = this.modal_data.delete.uid
      this.set_busy(true)
      console.log(`DELETE ${endpoint}/${uid}`)
      API
        .delete(`/${endpoint}/${uid}`)
        .then(response => {
          this.set_busy(false)
          if (response.data) {
            console.log(response)
            this.makeToast(`Entry ${uid} deleted`, 'success', 'Delete success')
            this.refreshTable()
          } else {
            if(response.response.data.description) {
              this.makeToast(response.response.data.description, 'danger', 'An error occurred')
            } else {
              this.makeToast('Could not delete the entry', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
      this.$nextTick(() => {
        this.$bvModal.hide('delete')
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
    },
    search(query) {
      console.log(`Search: ${this.query}`)
      this.search_data = query
      this.refreshTable()
      this.add_history()
    },
    search_clear() {
      if (this.search_data != '') {
        this.search_data = ''
        this.refreshTable()
        this.add_history()
      }
    },
    changeTab(tab, refresh = true) {
      this.tab_index = this.tabs.indexOf(tab)
      this.filter = tab.filter
      if (tab.handler) {
        tab.handler(tab)
      }
      if (refresh) {
        this.refreshTable()
        this.add_history()
      }
    },
    refreshTable() {
      this.set_busy(true)
      this.refresh()
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
    select (items) {
      this.selected = items
      // Emit the selected rows from the `b-table`
      this.$emit('row-selected', items)
    },
    modal_add () {
      this.modal_data.add = {}
      if (this.tabs[this.tab_index]['parent']) {
        this.modal_data.add['parent'] = this.tabs[this.tab_index]['parent']
      }
      this.$bvModal.show('add')
    },
    modal_edit (item) {
      var new_item = JSON.parse(JSON.stringify(item))
      this.modal_data.edit = new_item
      this.$bvModal.show('edit')
    },
    modal_delete(item) {
      this.modal_data.delete = item
      this.$bvModal.show('delete')
    },
    modal_clear() {
      this.modal_data.add = {}
      this.modal_data.edit = {}
      this.modal_data.delete = {}
    },
    sortingChanged (ctx) {
      this.orderby = ctx.sortBy
      this.isascending = ctx.sortDesc
      this.refreshTable()
    },
    clearSelected() {
      this.$refs.table.clearSelected()
    },
    select_all() {
      this.$refs.table.selectAllRows()
    },
    makeToast(text, variant = null, title = null, position = 'b-toaster-top-right') {
      if (title == null) {
        switch (variant) {
          case 'success':
            title = 'Success!'
            break
          case 'danger':
            title = 'Error!'
            break
          default:
            title = ''
        }
      }
      this.$bvToast.toast(text, {
        title: title,
        variant: variant,
        solid: true,
        toaster: position,
      })
    },
    add_history() {
      const query = { tab: this.tabs[this.tab_index].title, s: (this.$refs.search.datavalue || ''), env_name: this.env_name, env_filter: encodeURIComponent(JSON.stringify(this.env_filter)) }
      if (this.$route.query.tab != query.tab || this.$route.query.s != query.s || this.$route.query.env_name != query.env_name) {
        this.$router.push({ path: this.$router.currentRoute.path, query: query })
      }
    },
    hasSlot(name) {
      return !!this.$slots[name] || !!this.$scopedSlots[name]
    },
    contextMenu(item, index, event) {
      event.preventDefault()
      this.item_copy = item
      this.$refs.contextmenu.hideAll()
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
    copy_clipboard(vm, event) {
      var method
      var fields = this.fields
      if (vm.$attrs.method == 'yaml') {
        method = yaml.dump
      } else if (vm.$attrs.method == 'json') {
        method = JSON.stringify
      } else {
        this.to_clipboard(yaml.dump(this.item_copy[vm.$attrs.value], { flowLevel: 0 }).slice(0, -1))
        return
      }
      if (vm.$attrs.full) {
        fields = {}
      }
      this.add_clipboard(this.item_copy, method, fields)
    },
  },
  watch: {
    current_page: function() {
      this.refreshTable()
    },
    per_page: function() {
      this.refreshTable()
    },
    $route() {
      this.$nextTick(this.reload);
    }
  },
}
</script>

<style>

.fix-nav {
  height: 100%;
}

</style>
