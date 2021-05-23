<template>
  <div class="animated fadeIn">
    <List
      endpoint="aggregaterule"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    />
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields } from '@/objects/AggregateRule.yaml'

export default {
  components: {
    List,
  },
  data () {
    return {
      form: form,
      fields: fields,
      tabs: [
        {title: 'Aggregate Rules', filter: []},
      ],
    }
  },
}
</script>
