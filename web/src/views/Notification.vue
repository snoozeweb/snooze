<template>
  <div class="animated fadeIn">
    <List
      ref="table"
      endpoint_prop="notification"
      :tabs_prop="tabs"
      @update="get_now"
      edit_mode
      delete_mode
      add_mode
      show_tabs
    >
      <template #details_side="row">
        <AuditLogs collection="notification" :object="row.item" />
      </template>
    </List>
  </div>
</template>

<script>
import AuditLogs from '@/components/AuditLogs.vue'
import List from '@/components/List.vue'

import moment from 'moment'

export default {
  components: {
    AuditLogs,
    List,
  },
  data () {
    return {
      tabs: this.get_tabs_default(),
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
        {title: 'Upcoming', filter: ["AND", ["NOT", match], ["OR", ['NOT', ['EXISTS', 'time_constraints.datetime']], ['>=', 'time_constraints.datetime.from', date]]]},
        {title: 'Expired', filter: ["AND", ["NOT", match], ['<=', 'time_constraints.datetime.until', date]]},
        {title: 'All', filter: []},
      ]
    },
  },
}
</script>
