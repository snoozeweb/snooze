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
    />
      </b-col>
    </b-row>
  </div>
</template>

<script>

import Base from './Base.vue'
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
      datavalue: this.value,
      options: [
        {text: 'Monday',    value: 1},
        {text: 'Tuesday',   value: 2},
        {text: 'Wednesday', value: 3},
        {text: 'Thursday',  value: 4},
        {text: 'Friday',    value: 5},
        {text: 'Saturday',  value: 6},
        {text: 'Sunday',    value: 7},
      ],
    }
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
