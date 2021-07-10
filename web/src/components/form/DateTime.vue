<template>
  <div>
    <b-row>
      <b-col>
        <VueCtkDateTimePicker label="From" id="fromDate" v-model="fromDate" :minute-interval=5 output-format="YYYY-MM-DDTHH:mm:ssZ" format="YYYY-MM-DD HH:mm" :color="main_color" :error="!fromDate"/>
      </b-col>
      <b-col>
        <VueCtkDateTimePicker label="To" id="untilDate" v-model="untilDate" :minute-interval=5 output-format="YYYY-MM-DDTHH:mm:ssZ" format="YYYY-MM-DD HH:mm" :color="main_color" :error="!untilDate"/>
      </b-col>
    </b-row>
  </div>
</template>

<script>

import Base from './Base.vue'
import VueCtkDateTimePicker from 'vue-ctk-date-time-picker'
import 'vue-ctk-date-time-picker/dist/vue-ctk-date-time-picker.css';
import moment from 'moment'

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
        return {'from': moment().format(), 'until': moment().add(1, 'hours').format()}
      }
    },
    options: {},
  },
  data() {
    return {
      fromDate: this.value['from'],
      untilDate: this.value['until'],
      main_color: '',
    }
  },
  mounted() {
    var bodystyle = window.getComputedStyle(document.body)
    this.main_color = bodystyle.getPropertyValue('--primary').trim()
  },
  watch: {
    fromDate: {
      handler: function () {
        this.$emit('input', {'from': this.fromDate, 'until': this.untilDate})
      },
      immediate: true
    },
    untilDate: function () {
      this.$emit('input', {'from': this.fromDate, 'until': this.untilDate})
    }
  }
}

</script>
