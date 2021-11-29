<template>
  <div>
    <Datepicker
      v-model="datavalue"
      format="yyyy-MM-dd HH:mm"
      previewFormat="yyyy-MM-dd HH:mm"
      :placeholder="placeholder"
      :inputClassName="datavalue != null ? 'form-control is-valid' : 'form-control is-invalid'"
      :weekStart="week_start"
      :closeOnAutoApply="false"
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

var now = moment().format()
var one_hour_later = moment().add(1, 'hours').format()
var default_object = {from: now, until: one_hour_later}
var week_start = moment().startOf('week').weekday()

export default {
  extends: Base,
  name: 'DateTime',
  components: {
    Datepicker,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {
      type: Object,
      default: function () {
        return default_object
      }
    },
    options: {},
    placeholder: {type: String, default: () => 'Select Date'}
  },
  data() {
    return {
      datavalue: [this.modelValue['from'] || now, this.modelValue['until'] || one_hour_later],
      main_color: '',
      week_start: week_start,
    }
  },
  mounted() {
    this.main_color = getStyle('--primary') || '#304ffe'
  },
  computed: {
    formatted_date () {
       if (this.datavalue != null) {
         return {from: moment(this.datavalue[0]).format("YYYY-MM-DDTHH:mmZ"), until: moment(this.datavalue[1]).format("YYYY-MM-DDTHH:mmZ")}
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
  }
}

</script>
