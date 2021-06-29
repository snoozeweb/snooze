<template>
  <div>
    <b-form-input v-model="datavalue" :disabled="disabled" type="password" :state="isIdentical" ref="pwd"/>
    <div class="pt-1"><b-form-input v-model="datavalue_repeat" :disabled="disabled" type="password" :state="isIdentical" aria-describedby="feedback" ref="pwd_confirm"/></div>
    <b-form-invalid-feedback id="feedback" :state="isIdentical">
      <label v-if="datavalue.length == 0 && datavalue_repeat.length == 0">Fields are required</label>
      <label v-else>Passwords are not identical</label>
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
  },
  data() {
    return {
      datavalue: this.value || '',
      datavalue_repeat: '',
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
  method () {
    if (this.datavalue) {
      this.$refs['pwd'].value = '***'
      this.$refs['pwd_confirm'].value = '***'
    }
  },
  computed: {
    isIdentical() {
      if (this.datavalue.length == 0 && this.datavalue_repeat.length == 0) {
        if (this.required) {
          return false
        } else {
          return null
        }
      } else {
       return this.datavalue == this.datavalue_repeat
      }
    }
  },
}

</script>
