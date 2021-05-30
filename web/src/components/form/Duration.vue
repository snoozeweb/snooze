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
import { pp_counter } from '@/utils/api'

export default {
  extends: Base,
  props: {
    'value': {type: Number, default: () => 0},
    'options': {type: Object, default: () => {}},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
  },
  data() {
    return {
      datavalue: this.value,
      pp_counter: pp_counter,
    }
  },
  methods: {
    reset() {
      this.datavalue = this.options['default'] || 86400
    },
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
        return "No TTL"
      } else {
        return this.pp_counter(this.datavalue)
      }
    }
  },
}

</script>
