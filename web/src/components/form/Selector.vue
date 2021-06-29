<template>
  <div>
    <b-form-select v-model="datavalue" :options="options" aria-describedby="feedback" :required="required" :state="checkField">
      <template #first>
        <b-form-select-option disabled v-if="!default_value && default_value != ''" value="">Please select an option</b-form-select-option>
      </template>
    </b-form-select>
    <b-form-invalid-feedback id="feedback" :state="checkField">
      Field is required
    </b-form-invalid-feedback>
  </div>
</template>

<script>

import Base from './Base.vue'

// Create a selector form
export default {
  extends: Base,
  props: {
    value: {
      type: [String, Number, Boolean],
    },
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Array,
    },
    default_value: {
      type: [String, Number, Boolean],
    },
    required: {
      type: Boolean,
      default: () => false
    },
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? '' : this.default_value) : this.value
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('input', this.datavalue)
      },
      immediate: true
    },
  },
  computed: {
    checkField () {
      if (!this.required) {
        return null
      } else {
        return this.required && this.datavalue != ''
      }
    }
  },
}

</script>
