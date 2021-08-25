<template>
  <div class="animated fadeIn">
    <List
      endpoint="snooze"
      order_by="time_constraints"
      :form="form"
      :fields="fields"
      :tabs="tabs"
      ref="table"
      edit_mode
      delete_mode
      add_mode
    >
        <template v-slot:cell(hits)="row">
          <router-link :to="get_link(dig(row.item, 'name'))">{{ dig(row.item, 'hits') }}</router-link>
        </template>
    </List>
  </div>
</template>

<script>
import moment from 'moment'
import dig from 'object-dig'

import List from '@/components/List.vue'

import { form, fields, default_sorting } from '@/objects/Snooze.yaml'

export default {
  components: {
    List,
  },
  mounted () {
    setInterval(this.get_now, 1000);
  },
  data () {
    return {
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
  },
}
</script>
