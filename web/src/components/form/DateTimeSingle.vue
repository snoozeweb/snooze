<template>
  <div>
    <CRow>
      <CCol>
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
        />
      </CCol>
    </CRow>
  </div>
</template>

<script>

import Base from './Base.vue'
import { getStyle } from '@coreui/utils/src'
import Datepicker from 'vue3-date-time-picker';
import 'vue3-date-time-picker/dist/main.css';
import moment from 'moment'

export default {
  extends: Base,
  name: 'DateTimeSingle',
  components: {
    Datepicker,
  },
  emits: ['update:modelValue'],
  props: {
    modelValue: {
      type: String,
      default: function () {
        return moment().format()
      }
    },
    options: {},
    placeholder: {type: String, default: () => 'Select Date'}
  },
  data() {
    return {
      datavalue: this.modelValue || moment().format(),
      week_start: moment().startOf('week').weekday(),
    }
  },
  mounted() {
    this.datavalue = this.modelValue || moment().format()
  },
  computed: {
    formatted_date () {
       if (this.datavalue != null) {
         return moment(this.datavalue).format("YYYY-MM-DDTHH:mmZ")
       } else {
         return ''
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
