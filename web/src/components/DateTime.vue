<template>
  <span
    style="white-space:pre"
  >{{ trimDate(date, show_secs) }}</span>
</template>

<script>
import { trimDate } from '@/utils/api'

export default {
  props: {
    date: {type: String},
    show_secs: {type: Boolean, default: () => false},
  },
  data () {
    return {
      trimDate: trimDate,
    }
  },
  methods: {
  },
}
</script>
