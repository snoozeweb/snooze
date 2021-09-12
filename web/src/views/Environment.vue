<template>
  <div class="animated fadeIn">
    <List
      endpoint="environment"
      :form="form"
      :fields="fields"
      :tabs="tabs"
      edit_mode
      delete_mode
      add_mode
    >
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields } from '@/objects/Environment.yaml'

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
        {title: 'Environments', filter: []},
      ],
    }
  },
}
</script>
