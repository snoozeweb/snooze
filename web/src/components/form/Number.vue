<template>
  <div>
    <b-form-input type="number" v-model="dataval" :disabled="disabled" aria-describedby="feedback" :required="required" :state="checkField"/>
    <b-form-invalid-feedback id="feedback" :state="checkField">
      Field is required
    </b-form-invalid-feedback>
  </div>
</template>

<script>
// @group Forms
// Class for inputing a number
import Base from './Base.vue'

export default {
  extends: Base,
  props: {
    'value': {type: Number, default: () => 0},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
  },
  data() {
    return {
      datavalue: this.value
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
        return this.required && this.value != ''
      }
    },
    dataval: {
      get: function () {
        return parseInt(this.datavalue)
      },
      set: function (val) {
        this.datavalue = parseInt(val)
      }
    },
  },
}

</script>
