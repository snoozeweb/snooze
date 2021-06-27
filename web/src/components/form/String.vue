<template>
  <div>
    <b-form-input v-model="datavalue" :disabled="disabled" aria-describedby="feedback" :required="required" :state="checkField" :placeholder="placeholder"/>
    <b-form-invalid-feedback id="feedback" :state="checkField">
      Field is required
    </b-form-invalid-feedback>
  </div>
</template>

<script>
// @group Forms
// Class for inputing a string
import Base from './Base.vue'

export default {
  extends: Base,
  props: {
    'value': {type: String, default: () => ''},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'placeholder': {type: String, default: () => ''},
    'default_value': {type: String, default: () => ''},
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
  computed: {
    checkField () {
      if (!this.required) {
        return null
      } else {
        return this.required && this.value != ''
      }
    }
  },
}

</script>
