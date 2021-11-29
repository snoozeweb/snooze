<template>
  <div>
    <CFormTextarea name="string" v-model="datavalue" :disabled="disabled" aria-describedby="feedback" :required="required" :invalid="required && !checkField" :valid="required && checkField" :placeholder="placeholder"/>
    <CFormFeedback invalid id="feedback" :state="checkField">
      Field is required
    </CFormFeedback>
  </div>
</template>

<script>

import Base from './Base.vue'

export default {
  extends: Base,
  emits: ['update:modelValue'],
  props: {
    'modelValue': {type: String, default: () => ''},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'placeholder': {type: String, default: () => ''},
    'default_value': {type: String, default: () => ''},
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? '' : this.default_value) : this.modelValue
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('update:modelValue', this.datavalue)
      },
      immediate: true
    },
  },
  computed: {
    checkField () {
      return this.datavalue != ''
    }
  },
}

</script>
