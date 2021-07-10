<template>
  <div class="animated fadeIn">
    <List
      endpoint="snooze"
      order_by="time_constraint"
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

import { form, fields, default_sorting } from '@/objects/Snooze.yaml'

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
        {title: 'Active', filter: [{type: 'date_in', expression: [moment().format("YYYY-MM-DDTHH:mm"), 'time_constraints']}]},
        {title: 'Upcoming', filter: [">", "time_constraint.from", moment().format("YYYY-MM-DDTHH:mm")]},
        {title: 'Expired', filter: ["<", "time_constraint.until", moment().format("YYYY-MM-DDTHH:mm")]},
        {title: 'All', filter: []},
      ],
    }
  },
}
</script>
