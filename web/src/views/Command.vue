<template>
  <div class="animated fadeIn">
    <List
      endpoint="command"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    >
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields } from '@/objects/Command.yaml'

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
        {title: 'Commands', filter: []},
      ],
    }
  },
}
</script>
