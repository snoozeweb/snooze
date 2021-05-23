<template>
  <div>
    <b-form-select v-model="datavalue">
      <option disabled value="">Please select an option</option>
      <option
        v-for="(display_name, name) in options"
        :key="name"
        :value="name"
      >{{ display_name }}</option>
    </b-form-select>
  </div>
</template>

<script>

import Base from './Base.vue'

// Create a selector form
export default {
  extends: Base,
  props: {
    value: {},
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Array,
    },
  },
  data() {
    return {
      datavalue: this.value
    }
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    }
  },
}

</script>
