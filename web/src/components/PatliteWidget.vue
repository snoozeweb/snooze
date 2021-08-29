<template>
  <div>
    <b-button-group v-b-popover.hover.bottom="timestamp" title="Last updated time">
      <b-button variant='outline-dark' disabled size="sm">{{ options.name }}</b-button>
      <b-button
        disabled
        v-for="(status, color) in patlite_status"
        v-bind:key="color"
        size="sm"
        :variant="getStatusVariant(color, status)"
      >
        <span v-if="status != 'off'">O</span>
        <span v-else-if="status == 'off'">X</span>
        <span v-else>?</span>
      </b-button>
  <!--
  <div v-if="timestamp != null">
    Last fetch at {{ timestamp }}
  </div>
  -->
      <b-button size="sm" v-b-tooltip.hover title="Reset" variant="info" @click="resetPatlite()"><i class="la la-redo-alt la-lg" /></b-button>
      <b-button
        size="sm"
        :variant="auto_refresh ? 'success' : ''"
        v-b-tooltip.hover
        :title="auto_refresh ? 'Auto Refresh ON':'Auto Refresh OFF'"
        @click="toggle_auto()"
        :pressed.sync="auto_refresh"
      ><i v-if="auto_refresh" class="la la-eye la-lg" /><i v-else class="la la-eye-slash la-lg" /></i></b-button>
    </b-button-group>
  </div>
</template>

<script>
import { API } from '@/api'
import moment from 'moment'

var default_options = {}

// Create a card fed by an API endpoint.
export default {
  name: 'PatliteWidget',
  props: {
    options: {type: Object, default: () => Object.assign({}, default_options)},
  },
  data () {
    return {
      patlite_status: {},
      auto_refresh: true,
      auto_interval: null,
      timestamp: null,
    }
  },
  mounted() {
    this.getPatliteStatus()
    this.toggle_auto()
  },
  methods: {
    refresh() {
      this.getPatliteStatus()
    },
    /**
     * Get the Patlite status from snooze server and update the `patlite_status` and `timestamp` variables
     */
    getPatliteStatus() {
      var parameters = 'host='+encodeURI(this.options.widget.subcontent.host)+'&port='+this.options.widget.subcontent.port
      console.log(`GET /patlite/status?${parameters}`)
      API
        .get(`/patlite/status?${parameters}`)
        .then(response => {
          if (response.data !== undefined) {
            this.patlite_status = response.data
            this.timestamp = moment().format()
          }
        })
    },
    /**
     * Get the variant to use for a given color and status (on/off/blinking)
     * @param {string} color - The color of the patlite (red/yellow/green/blue/white)
     * @param {string} stat - The status of the light  (on/off/blink1/blink2)
     */
    getStatusVariant(color, stat) {
      const COLOR_MAP = {
        red: 'danger',
        yellow: 'warning',
        green: 'success',
        blue: 'primary',
        white: 'secondary',
        alert: 'info',
      }
      var variant_color = COLOR_MAP[color]
      switch(stat) {
        case 'on':
        case 'blink1':
        case 'blink2':
          return variant_color
        case 'off':
          return `outline-${variant_color}`
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
      if (this.auto_refresh) {
        this.auto_interval = setInterval(this.refresh, 10000)
      } else {
        if (this.auto_interval) {
          clearInterval(this.auto_interval)
          this.auto_interval = null
        }
      }
    },
  },
}
</script>

<style lang="scss" scoped>

</style>
