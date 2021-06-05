<template>
  <span
    style="white-space:pre"
  >{{ trimDate(date, false) }}</span>
</template>

<script>
import { trimDate } from '@/utils/api'

export default {
  props: {
    date: {type: String},
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
