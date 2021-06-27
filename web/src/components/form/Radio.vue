<template>
  <div>
    <b-form-group v-slot="{ ariaDescribedby }" class="m-0">
      <b-form-radio-group :aria-describedby="ariaDescribedby" v-model="datavalue" :options="options" button-variant="outline-primary" buttons />
    </b-form-group>
  </div>
</template>

<script>

import Base from './Base.vue'

// Create a selector form
export default {
  extends: Base,
  props: {
    value: {
      type: [Object, String, Number, Boolean],
    },
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Array,
    },
    default_value: {
      type: [Object, String, Number, Boolean],
    },
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? '' : this.default_value) : this.value
    }
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    }
  },
}

</script>
