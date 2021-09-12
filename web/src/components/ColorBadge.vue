<template>
  <b-badge :style="gen_color(data)">{{ data }}</b-badge>
</template>

<script>

import { gen_color } from '@/utils/colors'

export default {
  name: 'ColorBadge',
  props: {
    // Array representing the condition
    data: {
      // Array (it accepts String for recursion, but is not meant to be used like this)
      type: String,
    },
  },
  data() {
    return {
      gen_color: gen_color,
    }
  },
}
</script>
