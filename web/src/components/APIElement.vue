<template>
  <span v-if="attr === undefined">{{ item }}</span>
  <span v-else-if="attr in item">{{ item[attr] }}</span>
</template>

<script>
import { API } from '@/api'

export default {
  props: {
    value: {},
    attr: undefined,
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
  },
  data() {
    return {
      item: {},
    }
  },
  mounted () {
    this.item = this.get_item()
  },
  methods: {
    get_item () {
      console.log(`GET /${this.endpoint}/${this.value}`)
      API
        .get(`/${this.endpoint}/${this.value}`)
        .then(response => {
          console.log(response)
          this.item = response.data.data[0]
          return response.data.data[0]
        })
    },
  },
}
</script>
