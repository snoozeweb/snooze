<template>
  <span
    style="white-space:pre"
  >{{ show(data) }}</span>
</template>

<script>
import { pp_countdown } from '@/utils/api'

export default {
  props: {
    data: {type: Object},
  },
  methods: {
    show(data) {
      var total = data.total || 1
      var delay = data.delay || 0
      var every = data.every || 0
      var ret = []
      if (total != 1) {
        ret.push(total + 'x')
      }
      if (delay > 0) {
        ret.push('After ' + pp_countdown(delay))
      }
      if (every > 0) {
        ret.push('Every ' + pp_countdown(every))
      }
      if (ret.length == 0) {
        ret.push('Once')
      }
      return ret.join(' ')
    },
  },
}
</script>
