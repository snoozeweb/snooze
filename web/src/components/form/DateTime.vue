<template>
  <div>
    <VueDatePicker
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
import VueDatePicker from '@vuepic/vue-datepicker';
import '@vuepic/vue-datepicker/src/VueDatePicker/style/main.scss';
import moment from 'moment'


export default {
  extends: Base,
  name: 'DateTime',
  components: {
    VueDatePicker,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {
      type: Object,
      default: function () {
        return {from: moment().format(), until: moment().add(1, 'hours').format()}
      }
    },
    options: {},
    placeholder: {type: String, default: () => 'Select Date'}
  },
  data() {
    return {
      now: moment().format(),
      one_hour_later: moment().add(1, 'hours').format(),
      week_start: moment().startOf('week').weekday(),
      datavalue: [this.modelValue['from'] || moment().format(), this.modelValue['until'] || moment().add(1, 'hours').format()],
    }
  },
  mounted() {
    this.datavalue = [this.modelValue['from'] || this.now, this.modelValue['until'] || this.one_hour_later]
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
