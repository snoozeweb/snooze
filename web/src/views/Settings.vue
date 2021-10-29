<template>
  <div class="animated fadeIn">
    <Card
      endpoint_prop="settings"
      :onSubmit="submit"
      ref="card"
    >
    </Card>
  </div>
</template>

<script>

import Card from '@/components/Card.vue'

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
}
</script>
