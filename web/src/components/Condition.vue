<template>
  <span v-if="data === undefined || data == ''">Always true</span>
  <span v-else-if="data.constructor.name == 'Array'">
    <span v-if="data[0] == 'SEARCH'">
      ( <b>{{ data[0] }}</b> <Condition :data="data[1]" /> )
    </span>
    <span v-else-if="data[0] != 'NOT'">
      ( <Condition :data="data[1]"/> <b>{{ data[0] }}</b> <Condition v-if="data[0] != 'EXISTS'" :data="data[2]" /> )
    </span>
    <span v-else>
      ( <b>{{ data[0] }}</b> <Condition :data="data[1]"/> )
    </span>
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
