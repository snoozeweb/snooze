<template>
  <div>
    <b-row>
      <b-col>
    <VueCtkDateTimePicker
      only-time
      id="fromTime"
      label="From"
      v-model="datavalue['from']"
      :minute-interval=5
      :output-format="output_format"
      format="HH:mm"
      formatted="HH:mm"
      :error="!datavalue['from']"
      :color="main_color"
    />
      </b-col>
      <b-col>
    <VueCtkDateTimePicker
      only-time
      id="untilTime"
      label="Until"
      v-model="datavalue['until']"
      :minute-interval=5
      :output-format="output_format"
      format="HH:mm"
      formatted="HH:mm"
      :error="!datavalue['until']"
      :color="main_color"
    />
      </b-col>
    </b-row>
  </div>
</template>

<script>

import Base from './Base.vue'
import { getStyle } from '@coreui/utils/src'
import VueCtkDateTimePicker from 'vue-ctk-date-time-picker'
import 'vue-ctk-date-time-picker/dist/vue-ctk-date-time-picker.css';
import moment from 'moment'

var output_format = "HH:mm:ssZ"

var now = moment().format(output_format)
var one_hour_later = moment().add(1, 'hours').format(output_format)
var default_object = {from: now, until: one_hour_later}

export default {
  extends: Base,
  components: { VueCtkDateTimePicker },
  props: {
    value: {type: Object, default: () => Object.assign({}, default_object)},
  },
  data() {
    return {
      output_format: output_format,
      datavalue: {from: this.value['from'] || now, until: this.value['until'] || one_hour_later},
      main_color: '',
    }
  },
  mounted() {
    this.main_color = getStyle('--primary') || '#304ffe'
  },
  watch: {
    datavalue: {
      handler() { this.$emit('input', this.datavalue) },
      immediate: true,
      deep: true,
    },
  },
}
</script>
