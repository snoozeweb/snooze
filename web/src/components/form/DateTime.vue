<template>
  <div>
    <b-row>
      <b-col>
        <VueCtkDateTimePicker
          label="From"
          id="fromDate"
          v-model="datavalue['from']"
          :minute-interval=5
          output-format="YYYY-MM-DDTHH:mm:ssZ"
          format="YYYY-MM-DD HH:mm"
          :color="main_color"
          :error="!datavalue['from']"
        />
      </b-col>
      <b-col>
        <VueCtkDateTimePicker
          label="To"
          id="untilDate"
          v-model="datavalue['until']"
          :minute-interval=5
          output-format="YYYY-MM-DDTHH:mm:ssZ"
          format="YYYY-MM-DD HH:mm"
          :color="main_color"
          :error="!datavalue['until']"
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

var now = moment().format()
var one_hour_later = moment().add(1, 'hours').format()
var default_object = {from: now, until: one_hour_later}

export default {
  extends: Base,
  name: 'DateTime',
  components: {
    VueCtkDateTimePicker,
  },
  props: {
    value: {
      type: Object,
      default: function () {
        return default_object
      }
    },
    options: {},
  },
  data() {
    return {
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
  }
}

</script>
