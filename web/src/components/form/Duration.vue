<template>
  <div>
    <CForm @submit.prevent class="row g-0">
      <CCol xs="auto">
        <CInputGroup>
          <CFormInput v-model="datavalue" :disabled="disabled" aria-describedby="feedback" :required="required" :invalid="required && !checkField" :valid="required && checkField" type="number" min="-1"/>
          <CTooltip content="Reset">
            <template #toggler="{ on }">
              <CButton v-on:click="reset" :disabled="disabled" color="info" @click.stop.prevent v-on="on">
                <i class="la la-redo-alt la-lg"></i>
              </CButton>
            </template>
          </CTooltip>
          <CInputGroupText>{{ converted }}</CInputGroupText>
        </CInputGroup>
      </CCol>
    </CForm>
    <CFormFeedback invalid>
      Field is required
    </CFormFeedback>
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
    'modelValue': {type: [String, Number]},
    'options': {type: Object, default: () => {}},
    'disabled': {type: Boolean, default: () => false},
    'required': {type: Boolean, default: () => false},
    'default_value': {type: Number},
  },
  emits: ['update:modelValue'],
  data() {
    return {
      datavalue: ([undefined, '', [], {}].includes(this.modelValue) ? (this.default_value == undefined ? 86400 : this.default_value) : this.modelValue).toString(),
      pp_countdown: pp_countdown,
      opts: this.options || {},
    }
  },
  methods: {
    reset() {
      this.datavalue = (this.default_value == undefined ? 86400 : this.default_value).toString()
    },
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
    converted () {
      var datavalue = parseInt(this.datavalue) || 0
      if (datavalue < 0) {
        return this.opts.negative_label || ''
      } else if (datavalue == 0) {
        return this.opts.zero_label || this.opts.negative_label || ''
      } else if (this.opts.custom_label != undefined) {
        return (this.opts.custom_label_prefix || '') + datavalue + (this.opts.custom_label || '')
      } else {
        return (this.opts.custom_label_prefix || '') + this.pp_countdown(datavalue)
      }
    }
  },
}

</script>
