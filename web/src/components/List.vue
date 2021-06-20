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

        <b-nav-form class="pl-2">
          <Search @search="search($event)" @clear="search_clear()" ref='search' />
        </b-nav-form>

        <b-nav-item class="ml-auto" link-classes="py-0 pr-0">
          <b-button-toolbar key-nav>

            <b-button-group class="mx-2" v-if="Array.isArray(selected) && selected.length">
              <!-- Slot for placing buttons that appear only when a selection is made -->
              <slot name="selected_buttons"></slot>
              <b-button @click="clearSelected">Clear selection</b-button>
              <b-button
                variant="danger"
                v-if="delete_mode"
                @click="delete_items(endpoint, selected)"
              >Delete selection</b-button>
            </b-button-group>

            <b-button-group>
              <!-- Slots for placing additional buttons in the header of the table -->
              <b-button @click="select_all">Select All</b-button>
              <slot name="head_buttons"></slot>
              <b-button v-if="add_mode" variant="success" @click="modal_add()">Add</b-button>
              <b-button @click="refresh(true)"><i class="la la-refresh la-lg"></i></b-button>
            </b-button-group>

          </b-button-toolbar>
        </b-nav-item>

      </b-nav>
    </b-card-header>
    <b-card-body class="p-2">
      <b-table
        ref="table"
        @row-selected="select"
        @sort-changed="sortingChanged"
        :fields="fields"
        :items="items"
        :no-local-sorting="true"
        :sort-by.sync="orderby"
        :sort-desc.sync="isascending"
        selectable
        select-mode="range"
        selectedVariant="info"
        striped
        small
        bordered
      >
        <template v-for="(_, slot) of $scopedSlots" v-slot:[slot]="scope">
          <!-- cell() slots for the b-table -->
          <slot :name="slot" v-bind="scope" />
        </template>

        <template v-slot:cell(timestamp)="row">
          <DateTime :date="dig(row.item, 'timestamp')" />
        </template>
        <template v-slot:cell(condition)="row">
          <Condition :data="dig(row.item, 'condition')" />
        </template>
        <template v-slot:cell(actions)="row">
          <Action :data="dig(row.item, 'actions')" />
        </template>
        <template v-slot:cell(fields)="row">
          <Field :data="dig(row.item, 'fields')" />
        </template>
        <template v-slot:cell(severity)="row">
          <Field :data="[dig(row.item, 'severity')]" colorize/>
        </template>
        <template v-slot:cell(ttl)="row">
          {{ dig(row.item, 'ttl') >= 0 ? countdown(dig(row.item, 'ttl') - timestamp + dig(row.item, 'date_epoch')) : '-' }}
        </template>
        <template v-slot:cell(capabilities)="row">
          <Field :data="dig(row.item, 'capabilities')" colorize/>
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
        <template v-slot:cell(roles)="row">
          <Field :data="(dig(row.item, 'roles') || []).concat(dig(row.item, 'static_roles') || [])" colorize/>
        </template>
        <template v-slot:cell(time_constraint.from)="row">
          <DateTime :date="dig(row.item, 'time_constraint', 'from')" />
        </template>
        <template v-slot:cell(time_constraint.until)="row">
          <DateTime :date="dig(row.item, 'time_constraint', 'until')" />
        </template>
        <template v-slot:cell(state)="row">
          <Field :data="[(dig(row.item, 'state') || 'open')]" colorize/>
        </template>
        <template v-slot:cell(enabled)="row">
          <Field :data="[(dig(row.item, 'enabled') == undefined || dig(row.item, 'enabled') == true) ? 'enabled' : 'disabled']" colorize/>
        </template>

        <template v-slot:cell(button)="row">
          <b-button-group>
            <!-- Action buttons -->
            <b-button size="sm" @click="row.toggleDetails"><i v-if="row.detailsShowing" class="la la-angle-up la-lg"/><i v-else class="la la-angle-down la-lg"/></b-button>
            <slot name="button" v-bind="row" />
            <b-button v-if="edit_mode" size="sm" @click="modal_edit(row.item)" variant="primary" v-b-tooltip.hover title="Edit"><i class="la la-pencil-alt la-lg"/></b-button>
            <b-button v-if="delete_mode" size="sm" @click="modal_delete(row.item)" variant="danger" v-b-tooltip.hover title="Delete"><i class="la la-trash la-lg"/></b-button>
          </b-button-group>
        </template>
        <template v-slot:row-details="row">
          <b-card body-class="p-2" bg-variant="light">
          <b-row>
            <b-col>
              <b-card header='Infos' header-class='text-center font-weight-bold' body-class='p-2'>
                <ul>
                  <li v-for="(value, key) in row.item" v-bind:key="key.id" v-if="key[0] != '_'">
                    <strong>{{ key }}:</strong> {{ value }}
                  </li>
                </ul>
              </b-card>
            </b-col>
            <slot name="details_side" v-bind="row"></slot>
          </b-row>
            <b-button size="sm" @click="row.toggleDetails"><i class="la la-angle-up la-lg"/></b-button>
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
    <template v-slot:modal-title>Edit</template>
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
    <template v-slot:modal-title>Add</template>
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
    <template v-slot:modal-title>Deleting this item</template>
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

  </div>
</template>

<script>
import dig from 'object-dig'
import moment from 'moment'
import { API } from '@/api'
import { get_data, pp_countdown, countdown, preprocess_data } from '@/utils/api'
import { join_queries } from '@/utils/query'
import Form from '@/components/Form.vue'
import Search from '@/components/Search.vue'
import Condition from '@/components/Condition.vue'
import Action from '@/components/Action.vue'
import Field from '@/components/Field.vue'
import DateTime from '@/components/DateTime.vue'

import { delete_items } from '@/utils/api'

// Create a table representing an API endpoint.
export default {
  name: 'List',
  components: {
    Condition,
    Action,
    Field,
    DateTime,
    Search,
    Form,
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
    add_mode: {type: Boolean, default: true},
    // Allow the `Edit` button in actions
    edit_mode: {type: Boolean, default: true},
    // Allow the `Delete` button in actions
    delete_mode: {type: Boolean, default: true},
    // The default key to order by
    order_by: {type: String, default: undefined},
    // Ascending (true) or Descending (false)
    is_ascending: {type: Boolean, default: true},
  },
  mounted () {
    this.reload()
    this.get_now()
    setInterval(this.get_now, 1000);
  },
  data () {
    return {
      dig: dig,
      pp_countdown: pp_countdown,
      countdown: countdown,
      preprocess_data: preprocess_data,
      get_data: get_data,
      join_queries: join_queries,
      alert_countdown: 0,
      timestamp: 0,
      delete_items: delete_items,
      filter: this.tabs[0].filter,
      tab_index: 0,
      search_data: [],
      per_page: 20,
      page_options: [20, 50, 100],
      nb_rows: 0,
      current_page: 1,
      items: [],
      adding_data: {},
      selected_data: {},
      selected: [],
      orderby: this.order_by,
      isascending: this.is_ascending,
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
        }
      }
      this.changeTab(tab, false)
      if (this.$route.query.s !== undefined) {
        this.$refs.search.datavalue = this.$route.query.s
        this.$refs.search.search()
      } else {
        this.$refs.search.datavalue = ''
        this.refreshTable()
      }
    },
    get_now() {
      this.timestamp = moment().unix()
    },
    refresh(feedback = false) {
      var query = join_queries([this.filter, this.search_data])
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.orderby !== undefined) { options["orderby"] = this.orderby }
      this.get_data(this.endpoint, query, options, feedback ? this.feedback_then_update : this.update_table, null)
    },
    feedback_then_update(response) {
      this.alert_countdown = 1
      this.update_table(response)      
    },
    checkForm(node) {
      return (node.getElementsByClassName('form-control is-invalid').length + node.getElementsByClassName('has-error').length) == 0
    },
    submit_edit(bvModalEvt) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.edit_form)) {
        this.makeToast('Form is invalid', 'danger', 'Error')
        return
      }
      var data = this.modal_data.edit
      var filtered_object = this.preprocess_data(data)
      console.log(`PUT /${this.endpoint}`)
      API
        .put(`/${this.endpoint}`, [filtered_object])
        .then(response => {
          if (response.data) {
            this.refreshTable()
            this.makeToast('Entry updated successfully', 'success')
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
    submit_add(bvModalEvt) {
      bvModalEvt.preventDefault()
      if (!this.checkForm(this.$refs.add_form)) {
        this.makeToast('Form is invalid', 'danger', 'Error')
        return
      }
      var data = this.modal_data.add
      var filtered_object = this.preprocess_data(data)
      console.log(`POST /${this.endpoint}`)
      API
        .post(`/${this.endpoint}`, [filtered_object])
        .then(response => {
          if (response.data) {
            if (response.data.data.rejected.length > 0) {
              this.makeToast('Duplicate entry found', 'danger', 'An error occurred')
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
    submit_delete() {
      var uid = this.modal_data.delete.uid
      console.log(`DELETE ${this.endpoint}/${uid}`)
      API
        .delete(`/${this.endpoint}/${uid}`)
        .then(response => {
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
    },
    update_table(response) {
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
      this.search_data = JSON.parse(query)
      if (this.search_data != null && !Array.isArray(this.search_data)) {
        var search_word = this.search_data
        var lookup = "MATCHES"
        this.search_data = []
        this.fields.concat(this.hidden_fields).forEach((field, ) => {
          lookup = (field.type == "array"? "CONTAINS" : "MATCHES")
          if (!field.unsearchable) {
            if (this.search_data.length == 0) {
              this.search_data = [lookup, field.key, search_word]
            } else {
              this.search_data = ["OR", [lookup, field.key, search_word], this.search_data]
            }
          }
        })
      }
      this.refreshTable()
      this.add_history()
    },
    search_clear() {
      this.search_data = []
      this.refreshTable()
      this.add_history()
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
      this.items = []
      this.refresh()
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
      const query = { tab: this.tabs[this.tab_index].title, s: this.$refs.search.datavalue }
      if (this.$route.query.tab != query.tab || this.$route.query.s != query.s) {
        this.$router.push({ path: this.$router.currentRoute.path, query: query })
      }
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
