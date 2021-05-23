<template>
  <div class="animated fadeIn">
    <List
      endpoint="snooze"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    >
    </List>
  </div>
</template>

<script>
import moment from 'moment'

import List from '@/components/List.vue'

import { form, fields } from '@/objects/Snooze.yaml'

export default {
  components: {
    List,
  },
  mounted () {
  },
  data () {
    return {
      form: form,
      fields: fields,
      tabs: [
        {title: 'Active', filter: ["AND",
            ["<", "time_constraint.from", moment().format("YYYY-MM-DDTHH:mm")],
            [">", "time_constraint.until", moment().format("YYYY-MM-DDTHH:mm")],
          ],
        },
        {title: 'Upcoming', filter: [">", "time_constraint.from", moment().format("YYYY-MM-DDTHH:mm")]},
        {title: 'Expired', filter: ["<", "time_constraint.until", moment().format("YYYY-MM-DDTHH:mm")]},
      ],
    }
  },
}
</script>
