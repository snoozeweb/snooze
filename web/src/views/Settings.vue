<template>
  <div class="animated fadeIn">
    <Card
      endpoint="settings"
      :tabs="tabs"
      :form="form"
      :onSubmit="submit"
      ref="card"
    >
    </Card>
  </div>
</template>

<script>

import Card from '@/components/Card.vue'
import { tabs, form } from '@/objects/Settings.yaml'

export default {
  components: {
    Card,
  },
  mounted () {
  },
  methods: {
    submit(data) {
      this.$refs.card.submit({'conf': data, 'reload': this.$refs.card.current_tab.reload})
    },
  },
  data () {
    return {
      tabs: tabs,
      form: form
    }
  },
}
</script>
