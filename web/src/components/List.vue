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
          <Search @search="search($event)" @clear="search_clear()" />
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
              <b-button @click="get_data(true)"><i class="la la-refresh la-lg"></i></b-button>
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
        <template v-slot:cell(capabilities)="row">
          <Field :data="dig(row.item, 'capabilities')" colorize/>
        </template>
        <template v-slot:cell(groups)="row">
          <Field :data="dig(row.item, 'groups')" />
        </template>
        <template v-slot:cell(method)="row">
          <Field :data="[dig(row.item, 'method')]" colorize/>
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

        <template v-slot:cell(button)="row">
          <b-button-group>
            <!-- Action buttons -->
            <b-button size="sm" @click="row.toggleDetails">{{ row.detailsShowing ? 'Less' : 'More' }}</b-button>
            <slot name="button" v-bind="row" />
            <b-button v-if="edit_mode" size="sm" @click="modal_edit(row.item)" variant="primary"><i class="la la-pencil-square la-lg"></i></b-button>
            <b-button v-if="delete_mode" size="sm" @click="modal_delete(row.item)" variant="danger"><i class="la la-trash la-lg"></i></b-button>
          </b-button-group>
        </template>
        <template v-slot:row-details="row">
          <b-card>
            <ul>
              <li v-for="(value, key) in row.item" v-bind:key="key.id" v-if="key[0] != '_'">
                <strong>{{ key }}:</strong> {{ value }}
              </li>
            </ul>
            <b-button size="sm" @click="row.toggleDetails">Less</b-button>
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
    okVariant="danger"
    size="xl"
    centered
  >
    <template v-slot:modal-title>Deleting this item</template>
    <p>{{ modal_data.delete }}</p>

  </b-modal>
  </div>
</template>

<script>
import dig from 'object-dig'
import { API } from '@/api'
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
    this.get_data()
  },
  data () {
    return {
      dig: dig,
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
    object_to_query(obj) {
      return Object.entries(obj).map(([key, val]) => `${key}=${encodeURIComponent(val)}`).join('&')
    },
    get_data(alert = false) {
      var filter = JSON.stringify(this.joinQueries([this.filter, this.search_data]))
      var query = {
        s: filter,
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: this.isascending,
      }
      if (this.orderby !== undefined) { query["orderby"] = this.orderby }
      var query_str = this.object_to_query(query)
      var url = `/${this.endpoint}/?${query_str}`
      console.log(`GET ${url}`)
      API
        .get(url)
        .then(response => {
          console.log(response)
          if (response.data) {
            this.update_table(response.data)
            if(alert) {
              this.makeToast('Refresh successful', 'success', 'Success')
            }
          } else {
            if(response.response.data.description) {
              this.makeToast(response.response.data.description, 'danger', 'An error occurred')
            } else {
              this.makeToast('Could not display the content', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
    },
    preprocess_data(data) {
      var filtered_object = Object.assign({}, data)
      Object.keys(filtered_object).forEach((key, ) => {
        if (key[0] == '_') {
          delete filtered_object[key]
        }
      })
      return filtered_object
    },
    checkForm(node) {
      return (node.getElementsByClassName('form-control is-invalid').length == 0)
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
      this.items = []
      this.nb_rows = response['count']
      var rows = response['data']
      rows.forEach(row => {
        if ( this.items.every(x => x['uid'] != row['uid']) ) {
          this.items.push(row)
        }
      })
    },
    joinQueries(queries) {
      var filtered_queries = queries.filter(function (el) {
        return el != null && el != "" && el != [];
      });
      if (filtered_queries.length == 0) {
        return []
      }
      return filtered_queries.reduce((memo, query) => {
        if (query != "" && query != []) {
          return ['AND', memo, query]
        } else {
          return memo
        }
      })
    },
    search(query) {
      console.log(`Search: ${query}`)
      this.search_data = JSON.parse(query)
      if (!Array.isArray(this.search_data)) {
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
    },
    search_clear() {
      this.search_data = []
      this.refreshTable()
    },
    changeTab(tab) {
      this.tab_index = this.tabs.indexOf(tab)
      this.filter = tab.filter
      if (tab.handler) {
        tab.handler(tab)
      }
      this.refreshTable()
    },
    refreshTable() {
      this.items = []
      this.get_data()
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
    makeToast(text, variant = null, title = null) {
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
      })
    },
  },
  watch: {
    current_page: function() {
      this.refreshTable()
    },
    per_page: function() {
      this.refreshTable()
    },
  },
}
</script>

<style>

.fix-nav {
  height: 100%;
}

</style>
