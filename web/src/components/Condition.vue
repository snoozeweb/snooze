<template>
  <span v-safe-html="htmlValue" />
</template>

<script>
import { ConditionObject } from '@/utils/condition'

export default {
  name: 'Condition',
  props: {
    // Array representing the condition
    data: {
      // Array (it accepts String for recursion, but is not meant to be used like this)
      type: Array,
    },
  },
  computed: {
    htmlValue () {
      return ConditionObject.fromArray(this.data).toHTML()
    },
  },
}
</script>
