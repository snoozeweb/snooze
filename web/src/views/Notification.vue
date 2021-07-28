<template>
  <div class="animated fadeIn">
    <List
      endpoint="notification"
      order_by="name"
      is_ascending="true"
      :form="form"
      :fields="fields"
      :tabs="tabs"
    />
  </div>
</template>

<script>
import List from '@/components/List.vue'

import { form, fields } from '@/objects/Notification.yaml'

export default {
  components: {
    List,
  },
  data () {
    return {
      form: form,
      fields: fields,
      tabs: [
        {title: 'Notifications', filter: []},
      ],
    }
  },
}
</script>
