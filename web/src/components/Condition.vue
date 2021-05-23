<template>
  <span v-if="data === undefined">Empty</span>
  <span v-else-if="data.constructor.name == 'Array'">
    ( <Condition :data="data[1]"/> <b>{{ data[0] }}</b> <Condition :data="data[2]" /> )
  </span>
  <span v-else-if="data.constructor.name == 'String'">{{ data }}</span>
  <span v-else>
    <i>Error in displaying condition: {{ data.constructor.name }}, data: {{ data }}</i>
  </span>
</template>

<script>

export default {
  name: 'Condition',
  props: {
    // Array representing the condition
    data: {
      // Array (it accepts String for recursion, but is not meant to be used like this)
      type: [Array, String],
    },
  },
}
</script>
