<template>
  <div class="animated fadeIn">
    <List
      endpoint="widget"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    >
    </List>
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields } from '@/objects/Widget.yaml'
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
        {title: 'Widgets', filter: []},
      ],
    }
  },
}
</script>
