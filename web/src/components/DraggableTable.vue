<template>
  <div class="animated fadeIn" >
    <CForm @submit.prevent="" class="pt-0 px-0 pb-0">
      <Search @search="search" v-model="search_value" @clear="search_clear" ref='search'>
        <template #search_buttons>
          <!-- Slots for placing additional buttons in the header of the table -->
          <template v-if="Array.isArray(selected) && selected.length > 0">
            <slot name="selected_buttons"></slot>
            <CButton
              color="danger"
              @click="modal_show('delete', selected.map(x => x.item))"
            >Delete selection</CButton>
          </template>
          <slot name="head_buttons"></slot>
          <CButton color="success" @click="modal_show('new')">New</CButton>
          <CButton @click="refresh_table(true)" color="secondary" style="border-bottom-right-radius: 0"><i class="la la-refresh la-lg"></i></CButton>
        </template>
      </Search>
    </CForm>
    <div class="border" style='font-weight:bold'>
      <div class="d-flex">
        <div style="width: auto" class="align-middle p-1 singleline">
          <i v-if="!$route.query.s" class="la la-bars la-lg pe-1" style="visibility:hidden"></i>
          <input type="checkbox" class="pointer mx-1 me-1" :checked="selected.length == items.length" @change="toggle_check_all">
        </div>
        <div v-for="field in fields" :class="field.tdClass" :style="field.tdStyle">
          {{ capitalizeFirstLetter(field.label != undefined ? field.label : field.key) }}
        </div>
      </div>
    </div>
    <div class="border striped-bg p-2" style='font-weight:bold' v-if="!items.length">
      <slot name="no-items-view">
        <div class="text-center my-0">
          <h3 v-if="!is_busy" class="mb-0">
              No items
            <i
              width="20"
              class="la la-ban text-danger mb-2"
            ></i>
          </h3>
          <h3 class="mb-0" v-else>
            Loading...
          </h3>
        </div>
      </slot>
    </div>
    <Draggable ref="draggable" :flatData="items" idKey="nid" parentIdKey="pid" @drop-change="drag_end" triggerClass="can-drag" @mouseover="can_drop = true" @mouseleave="can_drop = false" :afterPlaceholderCreated="store_placeholder" :eachDroppable="eachDroppable">
      <template v-slot="{ node, tree }">
        <div v-if="node.item.date_epoch">
        <div :class="rowClass(node)" @mouseover="node.hover = true" @mouseleave="node.hover = false" @contextmenu="contextMenu(node.item, $event)" v-contextmenu:contextmenu>
          <div class="d-flex">
            <div style="width: auto" class="d-flex align-items-center align-middle p-1 singleline">
              <i v-if="!$route.query.s" :class="['la la-bars la-lg can-drag pe-1', tree.dragging ? '' : 'grab']"></i>
              <input type="checkbox" class="pointer ms-1 me-1" :checked="dig(node, '_checked')" @change="check(node)">
            </div>
            <div v-for="field in fields" :class="field.tdClass" :style="field.tdStyle">
              <Condition v-if="field.key == 'condition'" :data="dig(node.item, 'condition')" />
              <Condition v-else-if="field.key == 'filter'" :data="dig(node.item, 'filter')" />
              <div v-else-if="field.key == 'group'">{{ dig(node.item, 'group') || '0' }}</div>
              <Modification v-else-if="field.key == 'modifications'" :data="dig(node.item, 'modifications')" />
              <ColorBadge v-else-if="field.key == 'color'" :data="dig(node.item, 'color') || '#ffffff'" />
              <span v-else>{{ dig(node.item, field.key) }}</span>
            </div>
            <div :class="['float-right', 'position-relative', {'d-none': !node.hover}]">
              <div style="position: absolute; right: 0px; top: 50%; transform: translateY(-50%)">
                <CButtonGroup role="group">
                  <CButton color="secondary" size="sm" @click="toggleDetails(node.item, $event)">
                    <i v-if="Boolean(dig(node.item, '_showDetails'))" class="la la-angle-up la-lg"></i>
                    <i v-else class="la la-angle-down la-lg"></i>
                  </CButton>
                  <CButton v-if="max_level != 1" size="sm" @click="modal_show('new', [{'parent': dig(node.item, 'uid')}])" color="success" v-c-tooltip="{content: 'Add child'}"><i class="la la-plus la-lg"></i></CButton>
                  <CButton size="sm" @click="modal_show('edit', [node.item])" color="primary" v-c-tooltip="{content: 'Edit'}"><i class="la la-pencil-alt la-lg"></i></CButton>
                  <CButton size="sm" @click="modal_show('delete', [node.item])" color="danger" v-c-tooltip="{content: 'Delete'}"><i class="la la-trash la-lg"></i></CButton>
                </CButtonGroup>
              </div>
            </div>
          </div>
        </div>
        <CCard v-if="Boolean(dig(node.item, '_showDetails'))">
          <CRow class="m-0">
            <CCol class="p-2">
              <slot name="info" v-bind="node" />
              <Info :myobject="node.item" :excluded_fields="info_excluded_fields" />
            </CCol>
            <slot name="details_side" v-bind="node" />
          </CRow>
          <CButton size="sm" @click="toggleDetails(node.item, $event)"><i class="la la-angle-up la-lg"></i></CButton>
        </CCard>
        </div>
        <div v-else></div>
      </template>
    </Draggable>
    <div class="d-flex align-items-center pt-2" v-if="!no_paging && nb_rows > per_page">
      <div class="me-2">
        <SPagination
          :activePage="current_page"
          @update:activePage="change_currentpage"
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
              @change="change_perpage"
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

    <CModal
      ref="modal"
      :visible="show_modal"
      @close="modal_clear"
      alignment="center"
      size="xl"
      backdrop="static"
    >
      <CModalHeader :class="`bg-${modal_bg_variant}`">
        <CModalTitle v-if="modal_type != 'delete'" :class="`text-${modal_text_variant}`">{{ modal_title }}</CModalTitle>
        <CModalTitle v-else-if="modal_data.length > 1" :class="`text-${modal_text_variant}`">Delete {{ modal_data.length }} items</CModalTitle>
        <CModalTitle v-else :class="`text-${modal_text_variant}`">Delete this item</CModalTitle>
      </CModalHeader>
      <CModalBody>
        <CForm v-if="modal_type != 'delete'" @submit.stop.prevent="check_form" novalidate ref="form">
          <Form v-model="modal_data" :metadata="form" :footer_metadata="form_footer"/>
        </CForm>
        <p v-else-if="modal_data.length > 1">This operation cannot be undone. Are you sure?</p>
        <p v-else>{{ modal_data }}</p>
      </CModalBody>
      <CModalFooter>
        <CButton @click="modal_clear" color="secondary">Cancel</CButton>
        <CButton @click="modal_submit" :color="modal_bg_variant">OK</CButton>
      </CModalFooter>
    </CModal>

    <v-contextmenu ref="contextmenu" @show="store_selection">
      <v-contextmenu-item @click="copy_browser" v-if="selectedText">
        <i class="la la-copy la-lg"></i> Copy
      </v-contextmenu-item>
      <v-contextmenu-item @click="context_search" v-if="selectedText">
        <i class="la la-search la-lg"></i> Search
      </v-contextmenu-item>
      <v-contextmenu-submenu title="To Clipboard">
        <template v-slot:title><i class="la la-clipboard la-lg"></i> To Clipboard</template>
        <v-contextmenu-item @click="copy_clipboard(itemCopy, fields, $event)" method="yaml">
          As YAML
        </v-contextmenu-item>
        <v-contextmenu-item @click="copy_clipboard(itemCopy, fields, $event)" method="yaml" full="true">
          As YAML (Full)
        </v-contextmenu-item>
        <v-contextmenu-divider />
        <v-contextmenu-item @click="copy_clipboard(itemCopy, fields, $event)" method="json">
          As JSON
        </v-contextmenu-item>
        <v-contextmenu-item @click="copy_clipboard(itemCopy, fields, $event)" method="json" full="true">
          As JSON (Full)
        </v-contextmenu-item>
        <v-contextmenu-divider />
        <v-contextmenu-item v-for="field in fields.filter(field => field.key != 'button' && field.key != 'select')" :key="field.key" @click="copy_clipboard(itemCopy, fields, $event)" method="simple" :field="field.key">
          {{ capitalizeFirstLetter(field.key) }}
        </v-contextmenu-item>
      </v-contextmenu-submenu>
    </v-contextmenu>
  </div>
</template>

<script>

import dig from 'object-dig'
import '@he-tree/vue3/dist/he-tree-vue3.css'
import { Draggable } from '@he-tree/vue3'
import { get_data, add_items, update_items, delete_items, capitalizeFirstLetter, to_clipboard, copy_clipboard } from '@/utils/api'
import Form from '@/components/Form.vue'
import Search from '@/components/Search.vue'
import Condition from '@/components/Condition.vue'
import Modification from '@/components/Modification.vue'
import Field from '@/components/Field.vue'
import DateTime from '@/components/DateTime.vue'
import Info from '@/components/Info.vue'
import ColorBadge from '@/components/ColorBadge.vue'
import SPagination from '@/components/SPagination.vue'

export default {
  components: {
    Draggable,
    Form,
    Search,
    Condition,
    Modification,
    Field,
    DateTime,
    Info,
    ColorBadge,
    SPagination,
  },
  props: {
    endpoint_prop: {
      type: String,
      required: true,
    },
    max_level: {
      type: Number,
      default: 0,
    },
    default_search_prop: {type: String, default: ''},
    page_options_prop: {type: Array, default: () => ['20', '50', '100']},
    info_excluded_fields: {type: Array, default: () => []},
  },
  data () {
    return {
      busy_interval: null,
      is_busy: false,
      schema: {},
      checksum: null,
      loaded: false,
      endpoint: this.endpoint_prop,
      search_value: '',
      selected: [],
      form: {},
      form_footer: {},
      default_fields: this.fields_prop,
      fields: this.fields_prop,
      orderby: 'tree_order',
      isacending: false,
      default_search: this.default_search_prop,
      per_page: this.page_options_prop[0],
      page_options: this.page_options_prop,
      nb_rows: 0,
      current_page: 1,
      items: [],
      show_modal: false,
      selectedText: '',
      itemCopy: {},
      can_drop: false,
      placeholder: null,
      modal_title: '',
      modal_message: null,
      modal_type: '',
      modal_bg_variant: '',
      modal_text_variant: '',
      modal_data: {},
      to_clipboard: to_clipboard,
      copy_clipboard: copy_clipboard,
      capitalizeFirstLetter: capitalizeFirstLetter,
      add_items: add_items,
      update_items: update_items,
      delete_items: delete_items,
      dig: dig,
    }
  },
  mounted () {
    let storage = JSON.parse(localStorage.getItem(this.endpoint+'_json') || '{}')
    this.schema = storage.data
    var options = {}
    if (storage.checksum) {
      options.checksum = storage.checksum
    }
    get_data(`schema/${this.endpoint}`, [], options, this.load_table)
  },
  methods: {
    stripe () {
      var nodes = this.items
      nodes.forEach( (node, index) => {
        node._striped = index%2 == 0
      })
    },
    rowClass (row) {
      var classes = ['border']
      if (row._checked) {
        classes.push('table-info')
      } else if (row.item.enabled == false) {
        classes.push('table-dark')
      }
      if (row._striped) {
        classes.push('striped-bg')
      } else {
        classes.push('nonstriped-bg')
      }
      return classes
    },
    rowStyle (row) {
      if (row.pid == undefined && Array.isArray(row.item.parents) && row.item.parents.length) {
        return {'margin-left': `${row.item.parents.length*20}px`}
      } else {
        return {}
      }
    },
    should_indent (row) {
      return row.pid == undefined && Array.isArray(row.item.parents)
    },
    load_table (response) {
      // Cache was updated
      if (response.data.data) {
        this.schema = response.data.data
        localStorage.setItem(`${this.endpoint}_json`, JSON.stringify(response.data))
      }
      var data = this.schema
      if (data) {
        this.form = dig(data, 'form')
        this.form_footer = dig(data, 'form_footer')
        this.endpoint = dig(data, 'endpoint') || this.endpoint
        this.fields = dig(data, 'fields')
        this.isascending = dig(data, 'isascending') || false
        this.orderby = dig(data, 'orderby') || this.orderby
        this.fields.forEach(field => {
          field.tdClass = (field.tdClass || []).concat(['p-1', 'd-flex', 'border-color', 'align-items-center'])
          field.tdStyle = Object.assign(field.tdStyle || {}, {'border-left': '1px solid'})
        })
        this.default_fields = this.fields
        this.reload()
      }
    },
    reload () {
      var search = this.default_search
      if (this.$route.query.perpage !== undefined) {
        this.per_page = this.$route.query.perpage
      } else {
        this.per_page = this.page_options_prop[0]
      }
      if (this.$route.query.pagenb !== undefined) {
        this.current_page = parseInt(this.$route.query.pagenb)
      } else {
        this.current_page = 1
      }
      if (this.$route.query.s !== undefined) {
        search = decodeURIComponent(this.$route.query.s)
      }
      this.search_value = search
      if (this.$refs.search) {
        this.$refs.search.datavalue = search
      }
      this.refresh_table()
    },
    refresh_table(feedback = false) {
      this.uncheck_all()
      this.set_busy(true)
      var query = []
      var options = {
        perpage: this.per_page,
        pagenb: this.current_page,
        asc: false,
      }
      if (this.search_value) {
        if (this.search_value[0] == '[') {
          var search_json = JSON.parse(this.search_value)
          if (search_json) {
            query = join_queries([query, search_json])
          }
        } else {
          options["ql"] = this.search_value
        }
      }
      options["orderby"] = this.orderby
      options["asc"] = this.isascending
      get_data(this.endpoint, query, options, feedback == true ? this.feedback_then_update : this.update_table, null)
    },
    feedback_then_update(response) {
      this.$root.show_alert()
      this.update_table(response)
    },
    update_table(response) {
      this.set_busy(false)
      if (response.data) {
        this.items = []
        this.nb_rows = response.data.count
        var rows = response.data.data || []
        var item, tmp_item
        var index = 0
        rows.forEach(row => {
          if ( this.items.every(x => x['uid'] != row['uid']) ) {
            item = {'item': row}
            if (Array.isArray(row.parents) && row.parents.length > 0) {
              if (index == 0) {
                for (let i = 0; i < row.parents.length; i++) {
                  tmp_item = {'item': {'uid': row.parents[i], 'parents': i > 0 ? row.parents.slice(0, i) : []}, 'nid': index + 1, 'pid': index}
                  this.items.push(tmp_item)
                  index++
                }
              }
              if (this.items.length > 0) {
                for(let prev_index = 0; prev_index < index; prev_index++) {
                  if(row.parents[row.parents.length-1] == this.items[prev_index].item.uid) {
                    item.pid = prev_index + 1
                    break
                  }
                }
              }
            }
            item.nid = index + 1
            this.items.push(item)
          }
          index++
        })
        this.stripe()
      }
      if (!this.loaded) {
        this.loaded = true
      }
    },
    uncheck_all() {
      this.selected = []
      this.$refs.draggable.nodes.forEach(node => {
        node._checked = false
      })
    },
    check_all() {
      this.selected = []
      this.$refs.draggable.nodes.forEach(node => {
        node._checked = true
        this.selected.push(node)
      })
    },
    toggle_check_all () {
      if(this.selected.length == this.items.length) {
        this.uncheck_all()
      } else {
        this.check_all()
      }
    },
    check (node) {
      var found = this.selected.indexOf(node)
      if (found >= 0) {
        this.selected.splice(found, 1)
        node._checked = false
      } else {
        this.selected.push(node)
        node._checked = true
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
    add_history() {
      const query = { s: (this.search_value || this.default_search || ''), perpage: this.per_page, pagenb: this.current_page }
      if (this.$route.query.s != query.s || this.$route.query.perpage != query.perpage || this.$route.query.pagenb != query.pagenb) {
        this.$router.push({ query: query })
      }
    },
    toggleDetails(row, event) {
      event.stopPropagation()
      row._showDetails = !row._showDetails
    },
    search(query) {
      this.add_history()
    },
    search_clear() {
      this.search_value = this.default_search
      this.add_history()
    },
    modal_clear() {
      this.modal_data = {}
      this.modal_title = ''
      this.modal_message = null
      this.modal_type = ''
      this.modal_bg_variant = ''
      this.modal_text_variant = ''
      this.show_modal = false
      Array.from(document.getElementsByClassName('modal')).forEach(el => el.style.display = "none")
      Array.from(document.getElementsByClassName('modal-backdrop')).forEach(el => el.style.display = "none")
    },
    modal_show (type = '', items = [{}]) {
      this.modal_type = type
      switch (this.modal_type) {
        case 'edit':
          this.modal_title = 'Edit'
          this.modal_bg_variant = 'info'
          this.modal_text_variant = 'white'
          this.modal_data = JSON.parse(JSON.stringify(items[0]))
          break
        case 'delete':
          this.modal_title = 'Delete'
          this.modal_bg_variant = 'danger'
          this.modal_text_variant = 'white'
          this.modal_data = items
          break
        default:
          this.modal_title = 'New'
          this.modal_bg_variant = 'success'
          this.modal_text_variant = 'white'
          this.modal_data = items[0]
      }
      this.show_modal = true
    },
    check_form (node) {
      return (node.$el.getElementsByClassName('form-control is-invalid').length + node.$el.getElementsByClassName('has-error').length) == 0
    },
    modal_submit (bvModalEvt, endpoint = this.endpoint) {
      bvModalEvt.preventDefault()
      if (this.$refs.form && !this.check_form(this.$refs.form)) {
        this.$root.text_alert('Form is invalid', 'danger')
        return
      }
      this.set_busy(true)
      this.$nextTick(() => {
        this.modal_clear()
      })
      if (!Array.isArray(this.modal_data)) {
        this.modal_data = [this.modal_data]
      }
      switch (this.modal_type) {
        case 'edit':
          this.update_items(this.endpoint, this.modal_data, this.submit_callback)
          break
        case 'delete':
          this.delete_items(this.endpoint, this.modal_data, this.submit_callback)
          break
        default:
          this.add_items(this.endpoint, this.modal_data, this.submit_callback)
      }
    },
    submit_callback (response) {
      this.set_busy(false)
      this.refresh_table()
    },
    contextMenu (item, e) {
      this.itemCopy = item
      this.$refs.contextmenu.hide()
      this.$refs.contextmenu.show({top: e.pageY, left: e.pageX})
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
    drag_end (e) {
      if (this.can_drop) {
        this.set_busy(true)
        var draggingNode = this.$refs.draggable.draggingNode
        var parent = this.$refs.draggable.nodes.find(node => node.$id == draggingNode.$pid)
        var nodeIndex = this.$refs.draggable.nodes.findIndex(node => node.$id == draggingNode.$id)
        if (parent != undefined) {
          draggingNode.item.parent = parent.item.uid
        }
        if (nodeIndex == 0) {
          draggingNode.item.insert_before = this.$refs.draggable.visibleNodes[1].item.uid
        } else {
          draggingNode.item.insert_after = this.$refs.draggable.nodes[nodeIndex - 1].item.uid
        }
        this.update_items(this.endpoint, [draggingNode.item], this.submit_callback)
        delete draggingNode.item.parent
        delete draggingNode.item.insert_before
        delete draggingNode.item.insert_after
      } else {
        this.reload()
      }
    },
    change_perpage (e) {
      var new_value = parseInt(e.target.value)
      this.current_page = Math.ceil((this.current_page * this.per_page) / new_value)
      if (new_value != this.per_page) {
        this.per_page = new_value
        this.add_history()
      }
    },
    change_currentpage (page_number) {
      if (page_number != this.current_page) {
        this.current_page = page_number
        this.add_history()
      }
    },
    store_placeholder(p) {
      this.placeholder = p
    },
    eachDroppable (node, store, options, startTree) {
      if (this.max_level > 0 && node.$level >= this.max_level) {
        return false
      }
    },
  },
  watch: {
    can_drop: function() {
      if (this.placeholder && this.placeholder.children.length > 0) {
        if (!this.can_drop) {
          this.placeholder.children[0].className = 'bg-danger'
          this.placeholder.children[0].style.opacity = '0.5'
          this.placeholder.children[0].style.border = '1px dashed #fff'
        } else {
          this.placeholder.children[0].className = 'tree-placeholder tree-node'
          this.placeholder.children[0].style.opacity = ''
          this.placeholder.children[0].style.border = ''
        }
      }
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

</style>
