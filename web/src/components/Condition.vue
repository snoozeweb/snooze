<template>
  <span v-if="!rec && (data === undefined || data == '')">Always true</span>
  <span v-else-if="data.constructor.name == 'Array'">
    <span v-if="!rec && data[0] == ''">
      Always true
    </span>
    <span v-else-if="data[0] == 'SEARCH'">
      ( <b>{{ data[0] }}</b> <Condition :data="data[1]" rec /> )
    </span>
    <span v-else-if="data[0] != 'NOT'">
      ( <Condition :data="data[1]" rec /> <b>{{ data[0] }}</b> <Condition v-if="data[0] != 'EXISTS'" :data="data[2]" rec /> )
    </span>
    <span v-else>
      ( <b>{{ data[0] }}</b> <Condition :data="data[1]" rec /> )
    </span>
  </span>
  <span v-else>{{ data }}</span>
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
    rec: {type: Boolean, default: () => false},
  },
}
</script>
