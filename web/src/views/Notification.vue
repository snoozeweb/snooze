<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="notification"
      :tabs_prop="tabs"
      edit_mode
      delete_mode
      add_mode
    />
  </div>
</template>

<script>
import List from '@/components/List.vue'

import moment from 'moment'

export default {
  components: {
    List,
  },
  mounted () {
    setInterval(this.get_now, 1000);
  },
  data () {
    return {
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
        {title: 'Upcoming', filter: ["AND", ["NOT", match], ["OR", ['NOT', ['EXISTS', 'time_constraints.datetime']], ['>=', 'time_constraints.datetime.from', date]]]},
        {title: 'Expired', filter: ["AND", ["NOT", match], ['<=', 'time_constraints.datetime.until', date]]},
        {title: 'All', filter: []},
      ]
    },
  },
}
</script>
