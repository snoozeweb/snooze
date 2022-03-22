<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint_prop="snooze"
      :tabs_prop="tabs"
      @update="get_now"
      edit_mode
      delete_mode
      add_mode
      show_tabs
    >
      <template v-slot:hits="row">
        <router-link :to="get_link(dig(row.item, 'name'))">{{ dig(row.item, 'hits') || 0 }}</router-link>
      </template>
      <template #custom_buttons="row">
        <CButton color="info" @click="modal_show([row.item], 'apply')" size="sm" v-c-tooltip="{content: 'Retro apply'}"><i class="la la-redo la-lg"></i></CButton>
      </template>
      <template #selected_buttons>
        <CButton color="info" @click="modal_show(selected, 'apply')">Retro apply</CButton>
      </template>
      <template #details_side="row">
        <AuditLogs collection="snooze" :object="row.item" />
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
        <CModalTitle class="text-white" v-if="modal_data.length > 1">Retro apply to {{ modal_data.length }} items</CModalTitle>
        <CModalTitle :class="`text-${modal_text_variant}`" v-else>Retro apply</CModalTitle>
      </CModalHeader>
      <CModalBody>
        <p>{{ modal_message }}</p>
        <p>
          Retro applying on:
          <ul>
            <li v-bind:key="snooze_obj.name" v-for="snooze_obj in modal_data">{{ snooze_obj.name }}</li>
          </ul>
        </p>
      </CModalBody>
      <CModalFooter>
        <CButton @click="modal_clear" color="secondary">Cancel</CButton>
        <CButton @click="retro_apply(modal_data)" :color="modal_bg_variant">OK</CButton>
      </CModalFooter>
    </CModal>
  </div>
</template>

<script>
import moment from 'moment'
import dig from 'object-dig'

import AuditLogs from '@/components/AuditLogs.vue'
import List from '@/components/List.vue'

import { API } from '@/api'

export default {
  components: {
    AuditLogs,
    List,
  },
  data () {
    return {
      modal_title: '',
      modal_message: '',
      modal_type: '',
      modal_bg_variant: '',
      modal_text_variant: '',
      modal_data: [],
      show_modal: false,
      dig: dig,
      tabs: this.get_tabs_default(),
    }
  },
  computed: {
    selected () {
      return this.$refs.table.selected
    }
  },
  methods: {
    get_now() {
      this.tabs = this.get_tabs_default()
      this.$refs.table.tabs = this.tabs
    },
    get_tabs_default() {
      var now = moment()
      var date = now.format("YYYY-MM-DDTHH:mm")
      var hour = now.format("HH:mm:ssZ")
      var weekday = now.day()
      var match = ['AND',
        ['OR', ['NOT', ['EXISTS', 'time_constraints.weekdays']], ['IN', ['IN', weekday, 'weekdays'], 'time_constraints.weekdays']],
        ['AND',
          ['OR',
            ['NOT', ['EXISTS', 'time_constraints.datetime']],
            ['IN',
              ['AND', ['<=', 'from', date], ['>=', 'until', date]],
              'time_constraints.datetime'
            ]
          ],
          ['OR',
            ['NOT', ['EXISTS', 'time_constraints.time']],
            ['IN',
               ['AND', ['<=', 'from', hour], ['>=', 'until', hour]],
               'time_constraints.time'
            ]
          ]
        ]
      ]
      return [
        {title: 'Active', filter: match},
        {title: 'Upcoming', filter: ['AND', ['NOT', match], ['OR', ['NOT', ['EXISTS', 'time_constraints.datetime']], ['>=', 'time_constraints.datetime.until', date]]]},
        {title: 'Expired', filter: ['AND', ['NOT', match], ['AND', ['EXISTS', 'time_constraints.datetime'], ['NOT', ['>=', 'time_constraints.datetime.until', date]]]]},
        {title: 'All', filter: []},
      ]
    },
    get_link(name) {
      var escaped_name = JSON.stringify(name)
      return {
          path: 'record',
          query: {
          tab: 'Snoozed',
          s: encodeURIComponent(`snoozed=${escaped_name}`),
        },
      }
    },
    retro_apply(rows) {
      var filter_names = []
      rows.forEach(item => { filter_names.push(item.name) })
      console.log(`Retro apply ${filter_names}`)
      this.$refs.table.set_busy(true)
      API
        .put(`/snooze_apply`, filter_names)
        .then(response => {
          console.log(response)
          this.$refs.table.set_busy(false)
          if (response !== undefined && response.data !== undefined) {
            this.$root.text_alert(`Updated ${response.data} alert(s)`, 'success')
          } else {
            this.$root.text_alert('Could not apply snooze filter', 'danger')
          }
        })
        .catch(error => console.log(error))
      this.modal_clear()
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
      switch (type) {
        default:
          this.modal_title = 'Retro apply to all alerts'
          this.modal_message = 'This operation cannot be undone. Are you sure?'
          this.modal_bg_variant = 'info'
          this.modal_text_variant = 'white'
      }
      this.show_modal = true
    },
  },
}
</script>
