<template>
  <div class="animated fadeIn">
    <List
      endpoint="snooze"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    >
        <template v-slot:cell(hits)="row">
          <router-link :to="{ path: 'record', query: { tab: 'Snoozed', s: 'snoozed='+encodeURIComponent(dig(row.item,'name')) }}">{{ dig(row.item, 'hits') }}</router-link>
        </template>
    </List>
  </div>
</template>

<script>
import moment from 'moment'
import dig from 'object-dig'

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
      dig: dig,
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
