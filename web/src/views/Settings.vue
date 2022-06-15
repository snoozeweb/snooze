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
      this.$refs.card.submit(data)
    },
  },
}
</script>
