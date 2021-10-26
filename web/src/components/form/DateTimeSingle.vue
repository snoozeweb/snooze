<template>
  <div>
    <b-row>
      <b-col>
        <VueCtkDateTimePicker
          label="Date"
          id="Date"
          v-model="datavalue"
          :minute-interval=5
          output-format="YYYY-MM-DDTHH:mm:ssZ"
          format="YYYY-MM-DD HH:mm"
          :color="main_color"
          :error="!datavalue"
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

export default {
  extends: Base,
  name: 'DateTimeSingle',
  components: {
    VueCtkDateTimePicker,
  },
  props: {
    value: {
      type: String,
      default: moment().format(),
    },
    options: {},
  },
  data() {
    return {
      datavalue: this.value || now,
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
