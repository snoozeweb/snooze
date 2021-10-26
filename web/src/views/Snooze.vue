<template>
  <div class="animated fadeIn">
    <List
      endpoint="snooze"
      order_by="time_constraints"
      is_ascending
      :form="form"
      :fields="fields"
      :tabs="tabs"
      @row-selected="select"
      ref="table"
      edit_mode
      delete_mode
      add_mode
    >
      <template v-slot:cell(hits)="row">
        <router-link :to="get_link(dig(row.item, 'name'))">{{ dig(row.item, 'hits') }}</router-link>
      </template>
      <template #button="row">
        <b-button variant="info" @click="modal_show([row.item], 'apply')" size="sm" v-b-tooltip.hover title="Retro apply"><i class="la la-redo la-lg"></i></b-button>
      </template>
      <template #selected_buttons>
        <b-button variant="info" @click="modal_show(selected, 'apply')">Retro apply</b-button>
      </template>
    </List>

    <b-modal
      id="modal"
      ref="modal"
      @ok="retro_apply(modal_data)"
      @hidden="modal_clear()"
      :header-bg-variant="modal_bg_variant"
      :header-text-variant="modal_text_variant"
      size="xl"
      centered
    >
      <template #modal-title>{{ modal_title }}</template>
      <p>{{ modal_message }}</p>
    </b-modal>
  </div>
</template>

<script>
import moment from 'moment'
import dig from 'object-dig'

import List from '@/components/List.vue'

import { text_alert } from '@/utils/query'
import { form, fields, default_sorting } from '@/objects/Snooze.yaml'
import { API } from '@/api'

export default {
  components: {
    List,
  },
  mounted () {
    setInterval(this.get_now, 1000);
  },
  data () {
    return {
      modal_title: '',
      modal_message: '',
      modal_type: '',
      modal_bg_variant: '',
      modal_text_variant: '',
      modal_data: [],
      selected: [],
      dig: dig,
      form: form,
      fields: fields,
      tabs: this.get_tabs_default(),
    }
  },
  methods: {
    get_now() {
      this.tabs = this.get_tabs_default()
    },
    get_tabs_default() {
      var now = moment()
      var date = now.format("YYYY-MM-DDTHH:mm")
      var hour = now.format("HH:mm")
      var weekday = now.day()
      var match = ['AND',
        ['OR', ['NOT', ['EXISTS', 'time_constraints.weekdays']], ['IN', weekday, 'time_constraints.weekdays.weekdays']],
        ['AND',
          ['OR', ['NOT', ['EXISTS', 'time_constraints.datetime']], ['<=', 'time_constraints.datetime.from', date]],
          ['AND',
            ['OR', ['NOT', ['EXISTS', 'time_constraints.datetime']], ['>=', 'time_constraints.datetime.until', date]],
            ['AND',
              ['OR', ['NOT', ['EXISTS', 'time_constraints.time']], ['<=', 'time_constraints.time.from', hour]],
              ['OR', ['NOT', ['EXISTS', 'time_constraints.time']], ['>=', 'time_constraints.time.until', hour]]
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
      API
        .put(`/snooze_apply`, filter_names)
        .then(response => {
          console.log(response)
          if (response !== undefined && response.data) {
            text_alert(`Updated ${response.data} alert(s)`, 'Success', 'success')
          } else {
            text_alert('An error occured', 'Failure', 'danger')
          }
        })
        .catch(error => console.log(error))
    },
    select(items) {
      this.selected = items
    },
    modal_clear() {
      this.modal_data = []
      this.modal_title = ''
      this.modal_message = ''
      this.modal_type = ''
      this.modal_bg_variant = ''
      this.modal_text_variant = ''
    },
    modal_show(items, type) {
      this.modal_data = items
      this.modal_type = type
      this.modifications = []
      switch (type) {
        default:
          this.modal_title = 'Retro apply to all alerts'
          this.modal_message = 'This operation cannot be undone. Are you sure?'
          this.modal_bg_variant = 'info'
          this.modal_text_variant = 'white'
      }
      this.$bvModal.show('modal')
    },
  },
}
</script>
