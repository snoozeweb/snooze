<template>
  <div>
    <VueDatePicker
      v-model="datavalue"
      :placeholder="placeholder"
      :inputClassName="datavalue != null ? 'form-control is-valid' : 'form-control is-invalid'"
      :closeOnAutoApply="false"
      timePicker
      textInput
      autoApply
      range
    />
  </div>
</template>

<script>

import Base from './Base.vue'
import { getStyle } from '@coreui/utils/src'
import VueDatePicker from '@vuepic/vue-datepicker';
import '@vuepic/vue-datepicker/src/VueDatePicker/style/main.scss';
import moment from 'moment'

var output_format = "HH:mm:ssZ"

export default {
  extends: Base,
  components: { VueDatePicker },
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Object, default: () => Object.assign({}, {from: moment().format(output_format), until: moment().add(1, 'hours').format(output_format)})},
    placeholder: {type: String, default: () => 'Select Time'}
  },
  data() {
    return {
      now: moment().format(output_format),
      one_hour_later: moment().add(1, 'hours').format(output_format),
      datavalue: this.default_datavalue(),
    }
  },
  mounted() {
      this.datavalue = this.default_datavalue()
  },
  computed: {
    formatted_date () {
       if (this.datavalue != null) {
         return {
           from: moment('2000-01-01 ' + this.datavalue[0].hours + ':' + this.datavalue[0].minutes).format(output_format),
           until: moment('2000-01-01 ' + this.datavalue[1].hours + ':' + this.datavalue[1].minutes).format(output_format)
         }
       } else {
         return {}
       }
    }
  },
  methods: {
    default_datavalue () {
      return [
        {
          hours: moment('2000-01-01 ' + (this.modelValue['from'] || moment().format(output_format))).hours(),
          minutes: moment('2000-01-01 ' + (this.modelValue['from'] || moment().format(output_format))).minutes()
        }, {
          hours: moment('2000-01-01 ' + (this.modelValue['until'] || moment().add(1, 'hours').format(output_format))).hours(),
          minutes: moment('2000-01-01 ' + (this.modelValue['until'] || moment().add(1, 'hours').format(output_format))).minutes()
        }
      ]
    }
  },
  watch: {
    datavalue: {
      handler() { this.$emit('update:modelValue', this.formatted_date) },
      immediate: true,
      deep: true,
    },
  },
}
</script>
