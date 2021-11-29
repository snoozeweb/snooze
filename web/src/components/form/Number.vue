<template>
  <div>
    <CFormInput type="number" v-model="datavalue" :disabled="disabled" aria-describedby="feedback" :required="required" :invalid="required && !checkField" :valid="required && checkField"/>
    <CFormFeedback invalid>
      Field is required
    </CFormFeedback>
  </div>
</template>

<script>
// @group Forms
// Class for inputing a number
import Base from './Base.vue'

export default {
  extends: Base,
  props: {
    'modelValue': {type: [String, Number], default: () => 0},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'default_value': {type: Number, default: () => 0},
  },
  emits: ['update:modelValue'],
  data() {
    return {
      datavalue: ([undefined, 0, [], {}].includes(this.modelValue) ? (this.default_value == undefined ? 0 : this.default_value) : this.modelValue).toString()
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('update:modelValue', parseInt(this.datavalue) || 0)
      },
      immediate: true
    },
  },
  computed: {
    checkField () {
      return this.datavalue != ''
    },
  },
}

</script>
