<template>
  <div class="animated fadeIn">
    <List
      endpoint="action"
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

import { form, fields } from '@/objects/Action.yaml'
import { get_data, preprocess_data } from '@/utils/api'

export default {
  components: {
    List,
  },
  mounted () {
  },
  data () {
    return {
      get_data: get_data,
      form: form,
      fields: fields,
      tabs: [
        {title: 'Actions', filter: []},
      ],
    }
  },
}
</script>
