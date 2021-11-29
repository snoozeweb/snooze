<template>
  <div>
    <Datepicker
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
import Datepicker from 'vue3-date-time-picker';
import 'vue3-date-time-picker/dist/main.css';
import moment from 'moment'

var output_format = "HH:mm:ssZ"
var now = moment().format()
var one_hour_later = moment().add(1, 'hours').format()
var default_object = {from: now, until: one_hour_later}

export default {
  extends: Base,
  components: { Datepicker },
  emits: ['update:modelValue'],
  props: {
    modelValue: {type: Object, default: () => Object.assign({}, default_object)},
    placeholder: {type: String, default: () => 'Select Time'}
  },
  data() {
    return {
      datavalue: [
        {
          hours: moment(this.modelValue['from'] || now).hours(),
          minutes: moment(this.modelValue['from'] || now).minutes()
        }, {
          hours: moment(this.modelValue['until'] || one_hour_later).hours(),
          minutes: moment(this.modelValue['until'] || one_hour_later).minutes()
        }
      ],
      main_color: '',
    }
  },
  mounted() {
    this.main_color = getStyle('--primary') || '#304ffe'
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
  watch: {
    datavalue: {
      handler() { this.$emit('update:modelValue', this.formatted_date) },
      immediate: true,
      deep: true,
    },
  },
}
</script>
