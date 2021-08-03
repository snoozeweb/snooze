<template>
  <div class="animated fadeIn">
    <List
      endpoint="role"
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

import { form, fields } from '@/objects/Role.yaml'

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
        {title: 'Roles', filter: []},
      ],
    }
  },
}
</script>
