<template>
  <div>
    <CButtonGroup role="group">
      <CTooltip :content="timestamp" placement="bottom" trigger="hover">
        <template #toggler="{ on }">
          <div class='btn btn-outline-dark' style="cursor: auto; --cui-btn-hover-bg: inherit; --cui-btn-hover-color: inherit" v-on="on">{{ options.name }}</div>
        </template>
      </CTooltip>
      <div
        v-for="(status, color) in patlite_data"
        v-bind:key="color"
        style="cursor: auto; pointer-events: none"
        :class="['btn', getStatusVariant(color, status)].join(' ')"
      >
        <span v-if="status != 'off'">O</span>
        <span v-else-if="status == 'off'">X</span>
        <span v-else>?</span>
      </div>
      <CTooltip :content="patlite_status && patlite_status.message" placement="bottom" trigger="hover" v-if="patlite_status">
        <template #toggler="{ on }">
          <div
            style="cursor: auto; --cui-btn-hover-bg: inherit; --cui-btn-hover-color: inherit"
            class="btn btn-outline-danger"
            v-on="on"
          >
           {{ patlite_status.title }}
          </div>
        </template>
      </CTooltip>
  <!--
  <div v-if="timestamp != null">
    Last fetch at {{ timestamp }}
  </div>
  -->
      <CButton class="singleline" v-c-tooltip="{content: 'Clear', placement: 'bottom'}" color="info" @click="resetPatlite()">Clear <i class="la la-redo-alt la-lg"></i></CButton>
      <CTooltip :content="auto_mode ? 'Auto Refresh ON':'Auto Refresh OFF'" trigger="hover">
        <template #toggler="{ on }">
          <CButton :color="auto_mode ? 'success':'secondary'" @click="toggle_auto" v-on="on">
            <i v-if="auto_mode" class="la la-eye la-lg"></i>
            <i v-else class="la la-eye-slash la-lg"></i>
          </CButton>
        </template>
      </CTooltip>
    </CButtonGroup>
  </div>
</template>

<script>
import { API } from '@/api'
import moment from 'moment'

var default_options = {}
const COLOR_MAP = {
  red: 'danger',
  yellow: 'warning',
  green: 'success',
  blue: 'primary',
  white: 'secondary',
  alert: 'info',
}

// Create a card fed by an API endpoint.
export default {
  name: 'PatliteWidget',
  props: {
    options: {type: Object, default: () => Object.assign({}, default_options)},
  },
  data () {
    return {
      patlite_data: {},
      auto_mode: true,
      auto_interval: null,
      timestamp: 'No Data',
      patlite_status: null,
      timeout: null
    }
  },
  mounted() {
    this.getPatliteStatus()
    this.auto_interval = setInterval(this.refresh, 10000);
  },
  methods: {
    refresh() {
      this.getPatliteStatus()
    },
    /**
     * Get the Patlite status from snooze server and update the `patlite_data` and `timestamp` variables
     */
    getPatliteStatus() {
      this.timeout = setTimeout(() => {
        this.patlite_status = {title: 'Connecting to Patlite...', message: `Trying ${this.options.widget.subcontent.host}:${this.options.widget.subcontent.port}...`}
        this.timeout = null
      }, 1000)
      var parameters = 'host='+encodeURI(this.options.widget.subcontent.host)+'&port='+this.options.widget.subcontent.port
      console.log(`GET /patlite/status?${parameters}`)
      API
        .get(`/patlite/status?${parameters}`)
        .then(response => {
          if (this.timeout) {
            clearTimeout(this.timeout)
            this.timeout = null
          }
          if (response.data !== undefined) {
            this.patlite_data = response.data
            this.timestamp = moment().format()
            this.patlite_status = null
          } else {
            this.patlite_status = {title: 'Could not connect', message: response.message }
            this.timestamp = 'No Data'
          }
        })
    },
    /**
     * Get the variant to use for a given color and status (on/off/blinking)
     * @param {string} color - The color of the patlite (red/yellow/green/blue/white)
     * @param {string} stat - The status of the light  (on/off/blink1/blink2)
     */
    getStatusVariant(color, stat) {
      var variant_color = COLOR_MAP[color]
      switch(stat) {
        case 'on':
        case 'blink1':
        case 'blink2':
          return `btn-${variant_color}`
        case 'off':
          return `btn-outline-${variant_color}`
      }
    },
    /**
     * Order the snooze server to reset the Patlite status
     */
    resetPatlite() {
      var parameters = 'host='+encodeURI(this.options.widget.subcontent.host)+'&port='+this.options.widget.subcontent.port
      API
        .post(`/patlite/reset?${parameters}`)
        .then(response => {
          this.refresh()
        })
        .catch(error => console.log(error))
    },
    toggle_auto() {
      if(this.auto_interval) {
        clearInterval(this.auto_interval);
      }
      this.auto_mode = !this.auto_mode
      if (this.auto_mode) {
        this.auto_interval = setInterval(this.refresh, 10000);
      }
    },
  },
}
</script>

<style lang="scss" scoped>

</style>
