<template>
  <div>
    <b-form-select v-model="datavalue">
      <option disabled value="">{{ this.empty_message }}</option>
      <option v-for="item in this.items" :key="item[primary]" :value="item[primary]">
        {{ item['name'] }}
      </option>
    </b-form-select>
  </div>
</template>

<script>
import { API } from '@/api'
import Base from './Base.vue'

export default {
  extends: Base,
  props: {
    value: {},
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
    primary: {
      type: String,
      required: true,
    },
    subkey: {
      type: String,
    },
  },
  data() {
    return {
      datavalue: this.value,
      items: [],
      empty_message: "Please select a value",
    }
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    },
  },
  mounted () {
    this.reload_items()
  },
  methods: {
    reload_items () {
      console.log(`GET /${this.endpoint}`)
      API
        .get(`/${this.endpoint}`)
        .then(response => {
          console.log(response)
          if (response.data) {
            this.items = response.data.data
            if (this.subkey && this.items[this.subkey]) {
              this.items = this.items[this.subkey]
            }
          }
        })
    },
  },
}
</script>
