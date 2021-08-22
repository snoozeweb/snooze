<template>
  <div>
    <b-form inline>
      <b-input-group :append="converted">
    	<b-form-input v-model="dataval" :disabled="disabled" aria-describedby="feedback" :required="required" :state="checkField" type="number" min="-1"/>
        <b-input-group-append>
          <b-button v-on:click="reset" :disabled="disabled" variant="info"><i class="la la-redo-alt la-lg"></i></b-button>
        </b-input-group-append>
      </b-input-group>
    </b-form>
    <b-form-invalid-feedback id="feedback" :state="checkField">
      Field is required
    </b-form-invalid-feedback>
  </div>
</template>

<script>
// @group Forms
// Class for inputing a duration
import Base from './Base.vue'
import { pp_countdown } from '@/utils/api'

export default {
  extends: Base,
  props: {
    'value': {type: Number},
    'options': {type: Object, default: () => {}},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'default_value': {type: Number},
  },
  data() {
    return {
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? 86400 : this.default_value) : this.value,
      pp_countdown: pp_countdown,
    }
  },
  methods: {
    reset() {
      this.datavalue = this.default_value == undefined ? 86400 : this.default_value
    },
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
    converted () {
      if (this.datavalue < 0) {
        return "No expiration"
      } else {
        return this.pp_countdown(this.datavalue)
      }
    }
  },
}

</script>
