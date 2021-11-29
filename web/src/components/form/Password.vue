<template>
  <div>
    <CFormInput v-model="datavalue" :disabled="disabled" type="password" :invalid="checkFieldInvalid" :valid="checkFieldValid" ref="pwd"/>
    <div class="pt-1"><CFormInput v-model="datavalue_repeat" :disabled="disabled" type="password" :invalid="checkFieldInvalid" :valid="checkFieldValid" ref="pwd_confirm"/></div>
    <CFormFeedback invalid>
      <label v-if="datavalue.length == 0 && datavalue_repeat.length == 0">Fields are required</label>
      <label v-else>Passwords are not identical</label>
    </CFormFeedback>
  </div>
</template>

<script>
// @group Forms
// Class for inputing a string
import Base from './Base.vue'

export default {
  extends: Base,
  props: {
    'modelValue': {type: String, default: () => ''},
    'options': {type: Array, default: () => []},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
  },
  emits: ['update:modelValue'],
  data() {
    return {
      datavalue: this.modelValue || '',
      datavalue_repeat: '',
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
  methods: {
    checkField(is_valid) {
      if (this.datavalue.length == 0 && this.datavalue_repeat.length == 0) {
        return !is_valid && !this.required
      } else {
        return this.datavalue == this.datavalue_repeat
      }
    }
  },
  computed: {
    checkFieldValid() {
      return this.checkField(true)
    },
    checkFieldInvalid() {
      return !this.checkField(false)
    },
  },
}

</script>
